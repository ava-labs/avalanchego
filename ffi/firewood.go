// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Package ffi provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package ffi

//go:generate go run generate_cgo.go

// // Note that -lm is required on Linux but not on Mac.
// // FIREWOOD_CGO_BEGIN_STATIC_LIBS
// // #cgo linux,amd64 LDFLAGS: -L${SRCDIR}/libs/x86_64-unknown-linux-gnu
// // #cgo linux,arm64 LDFLAGS: -L${SRCDIR}/libs/aarch64-unknown-linux-gnu
// // #cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/libs/x86_64-apple-darwin
// // #cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/libs/aarch64-apple-darwin
// // FIREWOOD_CGO_END_STATIC_LIBS
// // FIREWOOD_CGO_BEGIN_LOCAL_LIBS
// #cgo LDFLAGS: -L${SRCDIR}/../target/debug
// #cgo LDFLAGS: -L${SRCDIR}/../target/release
// #cgo LDFLAGS: -L${SRCDIR}/../target/maxperf
// // FIREWOOD_CGO_END_LOCAL_LIBS
// #cgo LDFLAGS: -lfirewood_ffi -lm -ldl
// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// RootLength is the hash length for all Firewood hashes.
const RootLength = C.sizeof_HashKey

// Hash is the type used for all firewood hashes.
type Hash [RootLength]byte

// NodeHashAlgorithm represents the node hashing algorithm used by the database;
// this must match the algorithm previously used to crate the database.
//
// Currently, there are only two variants but more may be added in the future.
// At this time, the node hash algorithm used when opening the database must
// match the compile-time feature used when building the FFI library. This
// restriction will be lifted after #1088, which enables runtime selection of
// the node hashing algorithm.
type NodeHashAlgorithm C.enum_NodeHashAlgorithm

const (
	// MerkleDBNodeHashing uses MerkleDB-compatible SHA-256 based hashing
	MerkleDBNodeHashing NodeHashAlgorithm = C.NodeHashAlgorithm_MerkleDB
	// EthereumNodeHashing uses Ethereum-compatible Keccak-256 based hashing
	EthereumNodeHashing NodeHashAlgorithm = C.NodeHashAlgorithm_Ethereum
)

var (
	// EmptyRoot is the zero value for [Hash]
	EmptyRoot Hash
	// ErrActiveKeepAliveHandles is returned when attempting to close a database with unfreed memory.
	ErrActiveKeepAliveHandles = errors.New("cannot close database with active keep-alive handles")

	errDBClosed = errors.New("firewood database already closed")
)

// Database is an FFI wrapper for the Rust Firewood database.
// All functions rely on CGO to call into the underlying Rust implementation.
// Instances are created via [New] and must be closed with [Database.Close]
// when no longer needed.
//
// A Database can have outstanding [Revision] and [Proposal], which
// access the database's memory. These must be released before closing the
// database. See [Database.Close] for more details.
//
// Database supports two hashing modes: Firewood hashing and Ethereum-compatible
// hashing. Ethereum-compatible hashing is distributed, but you can use the more efficient
// Firewood hashing by compiling from source. See the Firewood repository for more details.
//
// For concurrent use cases, see each type and method's documentation for thread-safety.
type Database struct {
	// handle is returned and accepted by cgo functions. It MUST be treated as
	// an opaque value without special meaning.
	// https://en.wikipedia.org/wiki/Blinkenlights
	handle             *C.DatabaseHandle
	handleLock         sync.RWMutex
	outstandingHandles sync.WaitGroup

	// commitLock is used to ensure that methods accessing or modifying the latest
	// revision do not conflict.
	commitLock sync.Mutex
}

// config defines the internal configuration parameters used when opening a [Database].
type config struct {
	// truncate indicates whether to clear the database file if it already exists.
	truncate bool
	// nodeCacheEntries is the number of entries in the cache.
	// Must be non-zero.
	nodeCacheEntries uint
	// freeListCacheEntries is the number of entries in the freelist cache.
	// Must be non-zero.
	freeListCacheEntries uint
	// revisions is the maximum number of historical revisions to keep in memory.
	// If rootStoreDir is set, then any revisions removed from memory will still be kept on disk.
	// Otherwise, any revisions removed from memory will no longer be kept on disk.
	// Must be >= 2.
	revisions uint
	// readCacheStrategy is the caching strategy used for the node cache.
	readCacheStrategy CacheStrategy
	// rootStore defines whether to enable storing all historical revisions on disk.
	rootStore bool
	// expensiveMetricsEnabled controls whether expensive metrics recording is enabled.
	expensiveMetricsEnabled bool
	// deferredPersistenceCommitCount determines the maximum number of unpersisted
	// revisions that can exist at a given time.
	deferredPersistenceCommitCount uint64
}

func defaultConfig() *config {
	return &config{
		nodeCacheEntries:               1_000_000,
		freeListCacheEntries:           1_000_000,
		revisions:                      100,
		readCacheStrategy:              OnlyCacheWrites,
		deferredPersistenceCommitCount: 1,
	}
}

// Option is a function that configures a [Database].
type Option func(*config)

// WithTruncate sets whether to clear the database file if it already exists.
// Default: false
func WithTruncate(truncate bool) Option {
	return func(c *config) {
		c.truncate = truncate
	}
}

// WithNodeCacheEntries sets the number of entries in the node cache.
// The node cache stores frequently accessed trie nodes to improve read performance.
// Must be non-zero.
// Default: 1,000,000
func WithNodeCacheEntries(entries uint) Option {
	return func(c *config) {
		c.nodeCacheEntries = entries
	}
}

// WithFreeListCacheEntries sets the number of entries in the freelist cache.
// The freelist cache manages available disk space for reuse.
// Must be non-zero.
// Default: 1,000,000
func WithFreeListCacheEntries(entries uint) Option {
	return func(c *config) {
		c.freeListCacheEntries = entries
	}
}

// WithRevisions sets the maximum number of historical revisions to keep in memory.
// If RootStoreDir is set, then any revisions removed from memory will still be kept on disk.
// Otherwise, any revisions removed from memory will no longer be kept on disk.
// Must be >= 2.
// Default: 100
func WithRevisions(revisions uint) Option {
	return func(c *config) {
		c.revisions = revisions
	}
}

// WithReadCacheStrategy sets the caching strategy used for the node cache.
// Default: OnlyCacheWrites
func WithReadCacheStrategy(strategy CacheStrategy) Option {
	return func(c *config) {
		c.readCacheStrategy = strategy
	}
}

// WithRootStore defines whether to enable storing all historical revisions on disk.
// When set, historical revisions will be persisted to disk even after being
// removed from memory (based on the Revisions limit).
// Default: false
func WithRootStore() Option {
	return func(c *config) {
		c.rootStore = true
	}
}

// WithExpensiveMetrics enables expensive metrics recording for this database.
// Expensive metrics are disabled by default since they can introduce overhead.
// Default: false
func WithExpensiveMetrics() Option {
	return func(c *config) {
		c.expensiveMetricsEnabled = true
	}
}

// WithDeferredPersistenceCommitCount sets the maximum number of unpersisted revisions
// that can exist at a time. Note: `commitCount` must be greater than 0.
// Default: 1
func WithDeferredPersistenceCommitCount(commitCount uint64) Option {
	return func(c *config) {
		c.deferredPersistenceCommitCount = commitCount
	}
}

// A CacheStrategy represents the caching strategy used by a [Database].
type CacheStrategy uint8

const (
	// OnlyCacheWrites caches only writes.
	OnlyCacheWrites CacheStrategy = iota
	// CacheBranchReads caches intermediate reads and writes.
	CacheBranchReads
	// CacheAllReads caches all reads and writes.
	CacheAllReads

	// invalidCacheStrategy MUST be the final value in the iota block to make it
	// the smallest value greater than all valid values.
	invalidCacheStrategy
)

// New opens or creates a new Firewood database with the given node hashing
// algorithm and database options. The database directory will be created at the
// provided path if it does not already exist.
//
// The [nodeHashAlgorithm] is required and must match the compile-time feature:
//   - NodeHashAlgorithmEthereum if the ethhash feature is enabled
//   - NodeHashAlgorithmMerkleDB if the ethhash feature is disabled
//
// In the future, the node hash algorithm will be configurable at runtime and
// no longer need to match compile-time features but will instead select the
// desired hashing mode.
//
// If no [Option] is provided, sensible defaults will be used.
// See the With* functions for details about each configuration parameter and its default value.
//
// It is the caller's responsibility to call [Database.Close] when the database
// is no longer needed. No other [Database] in this process should be opened with
// the same file path until the database is closed.
func New(dbDir string, nodeHashAlgorithm NodeHashAlgorithm, opts ...Option) (*Database, error) {
	conf := defaultConfig()
	for _, opt := range opts {
		opt(conf)
	}

	if conf.readCacheStrategy >= invalidCacheStrategy {
		return nil, fmt.Errorf("invalid cache strategy (%d)", conf.readCacheStrategy)
	}
	if conf.revisions < 2 {
		return nil, fmt.Errorf("revisions must be >= 2, got %d", conf.revisions)
	}
	if conf.nodeCacheEntries < 1 {
		return nil, fmt.Errorf("node cache entries must be >= 1, got %d", conf.nodeCacheEntries)
	}
	if conf.freeListCacheEntries < 1 {
		return nil, fmt.Errorf("free list cache entries must be >= 1, got %d", conf.freeListCacheEntries)
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.struct_DatabaseHandleArgs{
		dir:                               newBorrowedBytes([]byte(dbDir), &pinner),
		cache_size:                        C.size_t(conf.nodeCacheEntries),
		free_list_cache_size:              C.size_t(conf.freeListCacheEntries),
		revisions:                         C.size_t(conf.revisions),
		strategy:                          C.uint8_t(conf.readCacheStrategy),
		truncate:                          C.bool(conf.truncate),
		root_store:                        C.bool(conf.rootStore),
		expensive_metrics:                 C.bool(conf.expensiveMetricsEnabled),
		node_hash_algorithm:               C.enum_NodeHashAlgorithm(nodeHashAlgorithm),
		deferred_persistence_commit_count: C.uint64_t(conf.deferredPersistenceCommitCount),
	}

	return getDatabaseFromHandleResult(C.fwd_open_db(args))
}

// Update applies a batch of operations to the database, returning the hash of the
// root node after the batch is applied. This is equivalent to creating a proposal
// with [Database.Propose], then committing it with [Proposal.Commit].
//
// Use [Put], [Delete], and [PrefixDelete] to create batch operations.
//
// This function conflicts with all other calls that access the latest state of the database,
// and will lock for the duration of this function.
func (db *Database) Update(batch []BatchOp) (Hash, error) {
	db.handleLock.RLock()
	defer db.handleLock.RUnlock()
	if db.handle == nil {
		return EmptyRoot, errDBClosed
	}

	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	var pinner runtime.Pinner
	defer pinner.Unpin()

	kvp := newKeyValuePairsFromBatch(batch, &pinner)

	return getHashKeyFromHashResult(C.fwd_batch(db.handle, kvp))
}

// Propose creates a new proposal with the given batch operations. The proposal
// is not committed until [Proposal.Commit] is called. See [Database.Close] regarding
// freeing proposals. All proposals should be freed before closing the database.
//
// Use [Put], [Delete], and [PrefixDelete] to create batch operations.
//
// This function conflicts with all other calls that access the latest state of the database,
// and will lock for the duration of this function.
func (db *Database) Propose(batch []BatchOp) (*Proposal, error) {
	db.handleLock.RLock()
	defer db.handleLock.RUnlock()
	if db.handle == nil {
		return nil, errDBClosed
	}

	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	var pinner runtime.Pinner
	defer pinner.Unpin()

	kvp := newKeyValuePairsFromBatch(batch, &pinner)
	return getProposalFromProposalResult(C.fwd_propose_on_db(db.handle, kvp), &db.outstandingHandles, &db.commitLock)
}

// Get retrieves the value for the given key from the most recent revision.
// If the key is not found, the return value will be nil.
//
// This function conflicts with all other calls that access the latest state of the database,
// and will lock for the duration of this function. If you need to perform concurrent reads,
// consider using [Database.Revision] or [Database.LatestRevision] to get a [Revision] and
// calling [Revision.Get] on it.
func (db *Database) Get(key []byte) ([]byte, error) {
	db.handleLock.RLock()
	defer db.handleLock.RUnlock()
	if db.handle == nil {
		return nil, errDBClosed
	}

	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	var pinner runtime.Pinner
	defer pinner.Unpin()

	val, err := getValueFromValueResult(C.fwd_get_latest(db.handle, newBorrowedBytes(key, &pinner)))
	// The revision won't be found if the database is empty.
	// This is valid, but should be treated as a non-existent key
	if errors.Is(err, errRevisionNotFound) {
		return nil, nil
	}

	return val, err
}

// Root returns the current root hash of the trie.
// With Firewood hashing, the empty trie must return [EmptyRoot].
// If the database is already closed, it returns [EmptyRoot].
//
// This function conflicts with all other calls that access the latest state of the database,
// and will lock for the duration of this function.
func (db *Database) Root() Hash {
	db.handleLock.RLock()
	defer db.handleLock.RUnlock()
	if db.handle == nil {
		return EmptyRoot
	}

	db.commitLock.Lock()
	defer db.commitLock.Unlock()
	return db.root()
}

// root assumes db.stateLock is held and the database is open.
func (db *Database) root() Hash {
	// Since we already guaranteed the database is open, we can ignore the error since the only error is that the handle is nil.
	hash, _ := getHashKeyFromHashResult(C.fwd_root_hash(db.handle))
	return hash
}

// LatestRevision returns a [Revision] representing the latest state of the database.
// If the latest revision has root [EmptyRoot], it returns an error. The [Revision] must
// be dropped prior to closing the database.
//
// This function conflicts with all other calls that access the latest state of the database,
// and will lock for the duration of this function.
func (db *Database) LatestRevision() (*Revision, error) {
	db.handleLock.RLock()
	defer db.handleLock.RUnlock()
	if db.handle == nil {
		return nil, errDBClosed
	}

	db.commitLock.Lock()
	defer db.commitLock.Unlock()
	root := db.root()
	if root == EmptyRoot {
		return nil, errRevisionNotFound
	}
	return db.Revision(root)
}

// Revision returns a historical revision of the database.
// If the provided root does not exist (or is the [EmptyRoot]), it returns an error.
// The [Revision] must be dropped prior to closing the database.
//
// This function is thread-safe with all other operations.
func (db *Database) Revision(root Hash) (*Revision, error) {
	db.handleLock.RLock()
	defer db.handleLock.RUnlock()
	if db.handle == nil {
		return nil, errDBClosed
	}

	rev, err := getRevisionFromResult(C.fwd_get_revision(
		db.handle,
		newCHashKey(root),
	), &db.outstandingHandles)
	if err != nil {
		return nil, err
	}

	return rev, nil
}

// Close releases the memory associated with the Database and stops the
// background persistence thread.
//
// This blocks until all outstanding keep-alive handles are disowned or the
// [context.Context] is cancelled. That is, until all Revisions and Proposals
// created from this Database are either unreachable or one of
// [Proposal.Commit], [Proposal.Drop], or [Revision.Drop] has been called on
// them. Unreachable objects will be automatically dropped before Close returns,
// unless an alternate GC finalizer is set on them.
//
// This is safe to call multiple times; subsequent calls after the first will do
// nothing.
func (db *Database) Close(ctx context.Context) error {
	db.handleLock.Lock()
	defer db.handleLock.Unlock()
	if db.handle == nil {
		return nil
	}

	done := make(chan struct{})
	go func() {
		db.outstandingHandles.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ctx.Err(), ErrActiveKeepAliveHandles)
	}

	db.commitLock.Lock()
	defer db.commitLock.Unlock()
	if err := getErrorFromVoidResult(C.fwd_close_db(db.handle)); err != nil {
		return fmt.Errorf("unexpected error when closing database: %w", err)
	}

	db.handle = nil // Prevent double free

	return nil
}

// Dump returns a DOT (Graphviz) format representation of the trie structure
// of the latest revision for debugging purposes.
//
// Returns an error if the database is closed or if there was an error
// dumping the trie.
func (db *Database) Dump() (string, error) {
	db.handleLock.RLock()
	defer db.handleLock.RUnlock()
	if db.handle == nil {
		return "", errDBClosed
	}

	bytes, err := getValueFromValueResult(C.fwd_db_dump(db.handle))
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

// FlushBlockReplay flushes buffered block replay operations to disk.
//
// This function is only meaningful when the FFI library was compiled with
// the `block-replay` feature and the `FIREWOOD_BLOCK_REPLAY_PATH` environment
// variable is set. Otherwise, it is a no-op.
//
// Note: the recording and flushing is not handled per db, and this will flush
// all logs, if multiple databases are open.
func FlushBlockReplay() error {
	return getErrorFromVoidResult(C.fwd_block_replay_flush())
}
