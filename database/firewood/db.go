//go:build cgo && !windows
// +build cgo,!windows

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"go.uber.org/zap"
)

const (
	// Name is the name of this database for database switches
	Name = "firewood"

	// DefaultFlushSize is the default number of operations before auto-flush
	DefaultFlushSize = 1000
)

// Database implements the database.Database interface using Firewood.
//
// Architecture: Batch-based adapter with auto-flush
// - Firewood uses proposal/commit pattern (batch operations)
// - database.Database expects immediate Put/Get operations
// - Adapter accumulates writes in pending batch
// - Auto-flushes when batch reaches threshold (default: 1000 ops)
// - ALSO flushes on periodic timer (default: 5 seconds) to prevent data loss on crash
// - Provides read-your-writes consistency by checking pending batch first
//
// Root Hash Tracking:
// - Firewood's Get() uses fwd_get_latest which requires an in-memory revision.
// - After restart, no revision exists in memory, so Get() returns nil for ALL keys.
// - Fix: We track the current root hash and use Revision(root).Get(key) which reads
//   directly from the persisted trie, bypassing the latest-revision system.
// - currentRoot is updated after every Propose+Commit under pendingMu lock.
//
// See ARCHITECTURE_NOTES.md for detailed design rationale.
type Database struct {
	fw          *ffi.Database
	log         logging.Logger
	dbPath      string // Path to this database (for debug logging)
	closed      atomic.Bool
	currentRoot ffi.Hash // Current trie root hash for GetFromRoot reads (protected by pendingMu)

	// readCacheGen is incremented on every flush/commit so that an in-flight Get()
	// that already snapshotted a (possibly stale) root can detect the flush and skip
	// caching its result, preventing a stale entry from entering the read cache.
	readCacheGen atomic.Uint64

	// Pending batch tracking for auto-flush
	// pendingMu is an RWMutex: reads (Get/Has/NewIterator) use RLock; writes (Put/Delete/flush) use Lock.
	pendingMu    sync.RWMutex
	pending      *pendingBatch // Accumulates writes until flush
	flushSize    int           // Auto-flush threshold
	flushOnClose bool          // Whether to flush pending writes on close

	// Periodic flush to prevent data loss on crash
	flushTicker *time.Ticker
	flushDone   chan struct{}

	// Go-level read cache: stores recently-read committed key-value pairs in Go
	// memory to avoid repeated FFI trie traversals for hot keys.
	// Access is protected by readCacheMu.  The cache is cleared on every
	// flush/batch-commit (readCacheGen is bumped at the same time).
	// Pending-batch entries always take priority in Get/Has, so the cache can
	// never hide an uncommitted write.
	readCacheMu  sync.RWMutex
	readCache    map[string][]byte
	readCacheMax int // 0 = cache disabled
}

// pendingBatch tracks writes that haven't been committed to Firewood yet
type pendingBatch struct {
	ops map[string]*pendingOp // key -> operation (using string key for map)
}

type pendingOp struct {
	key    []byte
	value  []byte // nil for delete
	delete bool
}

func newPendingBatch() *pendingBatch {
	return &pendingBatch{
		ops: make(map[string]*pendingOp),
	}
}

// New creates a new Firewood database instance.
//
// Parameters:
//   - file: Path to database directory
//   - configBytes: JSON-encoded Config (see config.go)
//   - log: Logger instance
//
// Returns database.Database implementation or error if initialization fails.
func New(file string, configBytes []byte, log logging.Logger) (database.Database, error) {
	// Start with defaults, then overlay config from JSON.
	// This ensures critical fields like RootStore default to true
	// even if the config file doesn't mention them.
	cfg := DefaultConfig()
	if len(configBytes) > 0 {
		// The configBytes contains the full db-config.json structure like:
		// {"leveldb": {...}, "firewood": {...}, "pruning": {...}}
		// We need to extract just the "firewood" section
		var fullConfig map[string]json.RawMessage
		if err := json.Unmarshal(configBytes, &fullConfig); err != nil {
			return nil, fmt.Errorf("failed to parse database config: %w", err)
		}

		// Extract the "firewood" section if it exists and overlay onto defaults
		if firewoodSection, exists := fullConfig["firewood"]; exists {
			if err := json.Unmarshal(firewoodSection, &cfg); err != nil {
				return nil, fmt.Errorf("failed to parse firewood config section: %w", err)
			}
		}
	}

	// Build FFI options from config
	options := []ffi.Option{
		ffi.WithNodeCacheSizeInBytes(cfg.CacheSizeBytes),
		ffi.WithFreeListCacheEntries(cfg.FreeListCacheEntries),
		ffi.WithRevisions(cfg.RevisionsInMemory),
		ffi.WithReadCacheStrategy(cfg.CacheStrategy),
	}

	// Enable root store for historical revision access.
	// Without this, old revisions are freed via the freelist and space is reused.
	// The latest state is always persisted regardless of this setting.
	if cfg.RootStore {
		options = append(options, ffi.WithRootStore())
	}

	// Open Firewood database. The firewood-go-ethhash/ffi package is compiled with
	// EthereumNodeHashing; this is the only supported algorithm in this build.
	fw, err := ffi.New(file, ffi.EthereumNodeHashing, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to open firewood database: %w", err)
	}

	// Snapshot current root hash for read operations.
	// After restart, no in-memory revision exists, so we track the persisted root
	// and use Revision(root).Get(key) to read directly from the committed trie.
	initialRoot := fw.Root()
	if err != nil {
		fw.Close(context.Background())
		return nil, fmt.Errorf("failed to get initial root hash: %w", err)
	}

	if initialRoot != ffi.EmptyRoot {
		log.Info("Firewood database opened with existing data",
			zap.Bool("rootStore", cfg.RootStore),
			zap.Uint("revisionsInMemory", cfg.RevisionsInMemory),
			zap.Uint("cacheSizeBytes", cfg.CacheSizeBytes),
			zap.String("rootHash", fmt.Sprintf("%x", initialRoot[:8])),
		)
	} else {
		log.Info("Firewood database opened (empty/new instance)",
			zap.Bool("rootStore", cfg.RootStore),
			zap.Uint("revisionsInMemory", cfg.RevisionsInMemory),
			zap.Uint("cacheSizeBytes", cfg.CacheSizeBytes),
		)
	}

	flushSize := cfg.FlushSize
	if flushSize == 0 {
		flushSize = DefaultFlushSize
	}

	readCacheMax := cfg.ReadCacheSize
	if readCacheMax < 0 {
		readCacheMax = 0
	}

	db := &Database{
		fw:           fw,
		log:          log,
		dbPath:       file,
		closed:       atomic.Bool{},
		currentRoot:  initialRoot,
		pending:      newPendingBatch(),
		flushSize:    flushSize,
		flushOnClose: true,
		flushTicker:  time.NewTicker(5 * time.Second), // Flush every 5 seconds
		flushDone:    make(chan struct{}),
		readCacheMax: readCacheMax,
		readCache:    make(map[string][]byte, readCacheMax),
	}

	// Start periodic flush goroutine to prevent data loss on crash
	go db.periodicFlush()

	return db, nil
}

// flushLocked commits pending writes to Firewood.
// Caller must hold pendingMu lock.
func (db *Database) flushLocked() error {
	if len(db.pending.ops) == 0 {
		return nil
	}

	// Build BatchOp slice for proposal
	batch := make([]ffi.BatchOp, 0, len(db.pending.ops))
	for _, op := range db.pending.ops {
		if op.delete {
			batch = append(batch, ffi.Delete(op.key))
		} else {
			batch = append(batch, ffi.Put(op.key, op.value))
		}
	}

	// Create and commit proposal
	proposal, err := db.fw.Propose(batch)
	if err != nil {
		return fmt.Errorf("firewood propose failed: %w", err)
	}
	if err := proposal.Commit(); err != nil {
		return fmt.Errorf("firewood commit failed: %w", err)
	}

	// Snapshot root after commit so subsequent reads see the new data.
	db.currentRoot = db.fw.Root()

	// Write-back verification: spot-check that committed data is readable.
	// Catches FFI bugs where Propose+Commit succeeds but data is silently lost.
	verifyCount := 0
	for _, op := range db.pending.ops {
		if verifyCount >= 3 {
			break
		}
		if op.delete {
			continue
		}
		readBack, err := db.getFromRoot(db.currentRoot, op.key)
		if err != nil || readBack == nil {
			db.log.Error("WRITE-BACK VERIFICATION FAILED: committed key not readable",
				zap.Int("keyLen", len(op.key)),
				zap.Error(err),
			)
			// Retry once
			retryProposal, retryErr := db.fw.Propose(batch)
			if retryErr != nil {
				return fmt.Errorf("firewood retry propose failed after verification failure: %w", retryErr)
			}
			if retryErr = retryProposal.Commit(); retryErr != nil {
				return fmt.Errorf("firewood retry commit failed after verification failure: %w", retryErr)
			}
			db.currentRoot = db.fw.Root()
			db.log.Warn("Firewood write-back verification: retry commit succeeded")
			break
		}
		verifyCount++
	}

	// Clear read cache: the trie root changed so all cached values are stale.
	// Bump the generation counter first so in-flight Gets that already snapshotted
	// the old root will notice the change and skip caching their (stale) results.
	if db.readCacheMax > 0 {
		db.readCacheGen.Add(1)
		db.readCacheMu.Lock()
		clear(db.readCache)
		db.readCacheMu.Unlock()
	}

	// Clear pending batch
	db.pending = newPendingBatch()

	db.log.Debug("Flushed pending batch")

	return nil
}


// getFromRoot reads a key from the trie at the given root hash.
// Returns (nil, nil) if the key is not present.
// Returns (nil, nil) if root is ffi.EmptyRoot (empty database).
func (db *Database) getFromRoot(root ffi.Hash, key []byte) ([]byte, error) {
	if root == ffi.EmptyRoot {
		return nil, nil
	}
	rev, err := db.fw.Revision(root)
	if err != nil {
		return nil, err
	}
	defer rev.Drop() //nolint:errcheck
	return rev.Get(key)
}

// Has implements database.KeyValueReader
func (db *Database) Has(key []byte) (bool, error) {
	if db.closed.Load() {
		return false, database.ErrClosed
	}

	// Read pending batch and snapshot root under read lock (non-blocking for concurrent Gets).
	db.pendingMu.RLock()
	op, inPending := db.pending.ops[string(key)]
	root := db.currentRoot
	db.pendingMu.RUnlock()

	// Check pending batch first (read-your-writes for uncommitted ops).
	if inPending {
		return !op.delete, nil
	}

	// Check committed state using root hash (works after restart).
	// Lock is released so concurrent Has/Get/FFI calls can proceed in parallel.
	val, err := db.getFromRoot(root, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

// Get implements database.KeyValueReader
// Provides read-your-writes consistency by checking pending batch first.
//
// Hot path: pending → read cache → FFI trie traversal.
// The pending check and root snapshot require only a brief read lock.
// The read cache and FFI call are performed without holding any lock, allowing
// true parallelism for concurrent Gets on the same database instance.
func (db *Database) Get(key []byte) ([]byte, error) {
	if db.closed.Load() {
		return nil, database.ErrClosed
	}

	keyStr := string(key)

	// Phase 1: check pending batch and snapshot root under a brief read lock.
	// This is the only phase that requires synchronization with writers.
	db.pendingMu.RLock()
	op, inPending := db.pending.ops[keyStr]
	root := db.currentRoot
	db.pendingMu.RUnlock()

	if inPending {
		if op.delete {
			return nil, database.ErrNotFound // Pending delete
		}
		// Return copy to prevent caller from modifying pending batch.
		result := make([]byte, len(op.value))
		copy(result, op.value)
		return result, nil
	}

	// Phase 2: check the Go-level read cache (no FFI, no trie traversal).
	// Snapshot the cache generation before the lookup so we can safely skip
	// caching the FFI result if a flush raced between Phase 2 and Phase 3.
	var genBefore uint64
	if db.readCacheMax > 0 {
		db.readCacheMu.RLock()
		cached, inCache := db.readCache[keyStr]
		genBefore = db.readCacheGen.Load()
		db.readCacheMu.RUnlock()
		if inCache {
			if cached == nil {
				return nil, database.ErrNotFound
			}
			result := make([]byte, len(cached))
			copy(result, cached)
			return result, nil
		}
	}

	// Phase 3: FFI trie traversal — no locks held, true parallel reads.
	value, err := db.getFromRoot(root, key)
	if err != nil {
		return nil, err
	}

	// Populate read cache so the next Get of this key skips the FFI call.
	// Skip if the cache generation changed (a flush occurred while we were in
	// the FFI call), because our result is based on a now-superseded root.
	if db.readCacheMax > 0 && db.readCacheGen.Load() == genBefore {
		db.readCacheMu.Lock()
		// Re-check generation and capacity under write lock.
		if db.readCacheGen.Load() == genBefore && len(db.readCache) < db.readCacheMax {
			if value != nil {
				valCopy := make([]byte, len(value))
				copy(valCopy, value)
				db.readCache[keyStr] = valCopy
			}
			// We intentionally do NOT cache nil (missing key) to avoid
			// returning a stale "not found" after a concurrent Put+flush.
		}
		db.readCacheMu.Unlock()
	}

	if value == nil {
		return nil, database.ErrNotFound
	}

	// Return copy to prevent caller from modifying Firewood's internal state
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Put implements database.KeyValueWriter
// Adds operation to pending batch and auto-flushes when threshold reached.
func (db *Database) Put(key []byte, value []byte) error {
	if db.closed.Load() {
		return database.ErrClosed
	}

	db.pendingMu.Lock()
	defer db.pendingMu.Unlock()

	// Make copies to prevent caller from modifying our internal state
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	// Add to pending batch
	db.pending.ops[string(keyCopy)] = &pendingOp{
		key:    keyCopy,
		value:  valueCopy,
		delete: false,
	}

	// Evict from read cache so a subsequent Get sees the pending value, not
	// the previously cached committed value.
	if db.readCacheMax > 0 {
		db.readCacheMu.Lock()
		delete(db.readCache, string(keyCopy))
		db.readCacheMu.Unlock()
	}

	// Auto-flush if threshold reached
	if len(db.pending.ops) >= db.flushSize {
		return db.flushLocked()
	}

	return nil
}

// Delete implements database.KeyValueDeleter
// Adds delete operation to pending batch and auto-flushes when threshold reached.
func (db *Database) Delete(key []byte) error {
	if db.closed.Load() {
		return database.ErrClosed
	}

	db.pendingMu.Lock()
	defer db.pendingMu.Unlock()

	// Make copy to prevent caller from modifying our internal state
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	// Add to pending batch as delete operation
	db.pending.ops[string(keyCopy)] = &pendingOp{
		key:    keyCopy,
		value:  nil,
		delete: true,
	}

	// Evict from read cache so a subsequent Get sees the pending delete.
	if db.readCacheMax > 0 {
		db.readCacheMu.Lock()
		delete(db.readCache, string(keyCopy))
		db.readCacheMu.Unlock()
	}

	// Auto-flush if threshold reached
	if len(db.pending.ops) >= db.flushSize {
		return db.flushLocked()
	}

	return nil
}

// NewBatch implements database.Batcher
// Returns a batch that accumulates operations and commits them atomically on Write().
// Note: Explicit batches do NOT auto-flush - only Write() commits them.
func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:  db,
		ops: make(map[string]*pendingOp),
	}
}

// preparePendingOps converts pending batch to sorted slice for merge iteration
// Caller must hold pendingMu lock
func (db *Database) preparePendingOpsLocked(start, prefix []byte) []pendingKV {
	if len(db.pending.ops) == 0 {
		return nil
	}

	// Convert map to slice
	pending := make([]pendingKV, 0, len(db.pending.ops))
	for _, op := range db.pending.ops {
		// Filter by prefix if specified
		if len(prefix) > 0 && !bytes.HasPrefix(op.key, prefix) {
			continue
		}
		// Filter by start if specified
		if len(start) > 0 && bytes.Compare(op.key, start) < 0 {
			continue
		}
		pending = append(pending, pendingKV{
			key:    op.key,
			value:  op.value,
			delete: op.delete,
		})
	}

	// Sort by key for merge iteration
	sort.Slice(pending, func(i, j int) bool {
		return bytes.Compare(pending[i].key, pending[j].key) < 0
	})

	return pending
}

// NewIterator implements database.Iteratee
// Returns native FFI trie iterator merging committed + pending operations
func (db *Database) NewIterator() database.Iterator {
	if db.closed.Load() {
		return newErrorIterator(database.ErrClosed)
	}

	db.pendingMu.RLock()
	defer db.pendingMu.RUnlock()

	pending := db.preparePendingOpsLocked(nil, nil)
	return newNativeIterator(db.fw, db.currentRoot, pending, nil, nil)
}

// NewIteratorWithStart implements database.Iteratee
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	if db.closed.Load() {
		return newErrorIterator(database.ErrClosed)
	}

	db.pendingMu.RLock()
	defer db.pendingMu.RUnlock()

	pending := db.preparePendingOpsLocked(start, nil)
	return newNativeIterator(db.fw, db.currentRoot, pending, start, nil)
}

// NewIteratorWithPrefix implements database.Iteratee
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	if db.closed.Load() {
		return newErrorIterator(database.ErrClosed)
	}

	db.pendingMu.RLock()
	defer db.pendingMu.RUnlock()

	pending := db.preparePendingOpsLocked(nil, prefix)
	return newNativeIterator(db.fw, db.currentRoot, pending, nil, prefix)
}

// NewIteratorWithStartAndPrefix implements database.Iteratee
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	if db.closed.Load() {
		return newErrorIterator(database.ErrClosed)
	}

	db.pendingMu.RLock()
	defer db.pendingMu.RUnlock()

	pending := db.preparePendingOpsLocked(start, prefix)
	return newNativeIterator(db.fw, db.currentRoot, pending, start, prefix)
}

// Compact implements database.Compacter
func (db *Database) Compact(start []byte, limit []byte) error {
	// Firewood is a merkle trie database - compaction may not be applicable
	// or could trigger internal optimization routines if available
	// TODO: Check if Firewood has compaction support
	return nil
}

// Close implements io.Closer
// Flushes pending writes and closes the underlying Firewood database.
func (db *Database) Close() error {
	if !db.closed.CompareAndSwap(false, true) {
		return database.ErrClosed
	}

	db.pendingMu.Lock()
	defer db.pendingMu.Unlock()

	// Flush any pending writes if configured to do so
	if db.flushOnClose && len(db.pending.ops) > 0 {
		db.log.Info("Flushing pending writes before close")
		if err := db.flushLocked(); err != nil {
			db.log.Error("Failed to flush pending writes on close")
			// Continue with close despite flush error
		}
	}

	// Stop periodic flush goroutine
	db.flushTicker.Stop()
	close(db.flushDone)

	// Close Firewood database
	ctx := context.Background()
	if err := db.fw.Close(ctx); err != nil {
		return fmt.Errorf("failed to close firewood database: %w", err)
	}

	db.log.Info("Firewood database closed")
	return nil
}

// periodicFlush runs in a background goroutine and flushes pending writes periodically
// This prevents data loss if the process crashes before the batch size threshold is reached
func (db *Database) periodicFlush() {
	for {
		select {
		case <-db.flushTicker.C:
			db.pendingMu.Lock()
			// Guard against a race where the timer fires just before Close()
			// stops the ticker, causing flushLocked() to call fw.Propose() on
			// an already-closed FFI database.
			if db.closed.Load() {
				db.pendingMu.Unlock()
				return
			}
			if len(db.pending.ops) > 0 {
				opsCount := len(db.pending.ops)
				if err := db.flushLocked(); err != nil {
					if db.log != nil {
						db.log.Error("Periodic flush failed",
							zap.Int("pendingOps", opsCount),
							zap.Error(err),
						)
					}
				} else if db.log != nil {
					db.log.Debug("Periodic flush committed pending writes",
						zap.Int("opsCount", opsCount),
					)
				}
			}
			db.pendingMu.Unlock()

		case <-db.flushDone:
			// Graceful shutdown
			return
		}
	}
}

// HealthCheck implements health.Checker with comprehensive database health monitoring
func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	if db.closed.Load() {
		return nil, database.ErrClosed
	}

	db.pendingMu.RLock()
	pendingOps := len(db.pending.ops)
	db.pendingMu.RUnlock()

	// Try a simple read operation to verify database is responsive.
	testKey := []byte("__health_check__")
	root := db.fw.Root()
	if _, err := db.getFromRoot(root, testKey); err != nil {
		return nil, fmt.Errorf("health check failed (read): %w", err)
	}

	return map[string]interface{}{
		"database":       "firewood",
		"status":         "healthy",
		"pendingOps":     pendingOps,
		"flushThreshold": db.flushSize,
	}, nil
}

// batch implements database.Batch for Firewood
// Operations are buffered in memory and committed atomically on Write().
type batch struct {
	db  *Database
	ops map[string]*pendingOp
}

func (b *batch) Put(key []byte, value []byte) error {
	// Make copies to prevent caller from modifying our internal state
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	b.ops[string(keyCopy)] = &pendingOp{
		key:    keyCopy,
		value:  valueCopy,
		delete: false,
	}
	return nil
}

func (b *batch) Delete(key []byte) error {
	// Make copy to prevent caller from modifying our internal state
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	b.ops[string(keyCopy)] = &pendingOp{
		key:    keyCopy,
		value:  nil,
		delete: true,
	}
	return nil
}

func (b *batch) Size() int {
	total := 0
	for _, op := range b.ops {
		total += len(op.key) + len(op.value)
	}
	return total
}

func (b *batch) Write() error {
	if b.db.closed.Load() {
		return database.ErrClosed
	}

	if len(b.ops) == 0 {
		return nil
	}

	// IMPORTANT: Flush database pending batch first to maintain consistency
	// This ensures batch operations see the latest state and don't conflict
	b.db.pendingMu.Lock()
	defer b.db.pendingMu.Unlock()

	if len(b.db.pending.ops) > 0 {
		if err := b.db.flushLocked(); err != nil {
			return fmt.Errorf("failed to flush pending before batch: %w", err)
		}
	}

	// Build BatchOp slice
	batchOps := make([]ffi.BatchOp, 0, len(b.ops))
	for _, op := range b.ops {
		if op.delete {
			batchOps = append(batchOps, ffi.Delete(op.key))
		} else {
			batchOps = append(batchOps, ffi.Put(op.key, op.value))
		}
	}

	// Create and commit proposal
	proposal, err := b.db.fw.Propose(batchOps)
	if err != nil {
		return fmt.Errorf("firewood batch propose failed: %w", err)
	}
	if err := proposal.Commit(); err != nil {
		return fmt.Errorf("firewood batch commit failed: %w", err)
	}

	// Snapshot root after commit
	b.db.currentRoot = b.db.fw.Root()

	// Clear read cache: trie root changed so all previously cached values are stale.
	if b.db.readCacheMax > 0 {
		b.db.readCacheGen.Add(1)
		b.db.readCacheMu.Lock()
		clear(b.db.readCache)
		b.db.readCacheMu.Unlock()
	}

	// Write-back verification: spot-check that batch data is readable after commit
	verifyCount := 0
	for _, op := range b.ops {
		if verifyCount >= 3 {
			break
		}
		if op.delete {
			continue
		}
		readBack, err := b.db.getFromRoot(b.db.currentRoot, op.key)
		if err != nil || readBack == nil {
			b.db.log.Error("BATCH WRITE-BACK VERIFICATION FAILED: committed key not readable",
				zap.Int("keyLen", len(op.key)),
				zap.Int("batchSize", len(b.ops)),
				zap.Error(err),
			)
			// Retry once
			retryProposal, retryErr := b.db.fw.Propose(batchOps)
			if retryErr != nil {
				return fmt.Errorf("firewood batch retry propose failed: %w", retryErr)
			}
			if retryErr = retryProposal.Commit(); retryErr != nil {
				return fmt.Errorf("firewood batch retry commit failed: %w", retryErr)
			}
			b.db.currentRoot = b.db.fw.Root()
			b.db.log.Warn("Firewood batch write-back verification: retry commit succeeded",
				zap.Int("batchSize", len(b.ops)),
			)
			break
		}
		verifyCount++
	}

	b.db.log.Debug("Batch write committed", zap.Int("keysWritten", len(b.ops)))

	return nil
}

func (b *batch) Reset() {
	b.ops = make(map[string]*pendingOp)
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range b.ops {
		if op.delete {
			if err := w.Delete(op.key); err != nil {
				return err
			}
		} else {
			if err := w.Put(op.key, op.value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}
