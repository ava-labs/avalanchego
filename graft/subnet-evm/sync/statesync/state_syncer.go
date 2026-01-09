// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/triedb"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/state/snapshot"

	syncclient "github.com/ava-labs/avalanchego/graft/subnet-evm/sync/client"
)

const (
	segmentThreshold       = 500_000 // if we estimate trie to have greater than this number of leafs, split it
	numStorageTrieSegments = 4
	numMainTrieSegments    = 8
	defaultNumWorkers      = 8

	// Request size bounds - prevents misconfiguration
	minRequestSize = 16    // Too small causes excessive round trips
	maxRequestSize = 10240 // Too large can cause timeouts and memory issues
)

var (
	errWaitBeforeStart     = errors.New("cannot call Wait before Start")
	errStateSyncStuck      = errors.New("state sync stuck, fallback to block sync required")
	errInvalidRequestSize  = errors.New("request size must be between 16 and 10240")
	errInvalidWorkerConfig = errors.New("NumCodeFetchingWorkers must be positive")
)

// ErrStateSyncStuck returns the sentinel error for stuck state sync detection.
// This allows external packages to check for this specific error condition.
func ErrStateSyncStuck() error {
	return errStateSyncStuck
}

// calculateOptimalWorkers determines the optimal number of sync workers based on available CPU cores.
// Uses 75% of available cores to leave headroom for other operations (networking, database, etc.).
// Enforces minimum of 4 workers and maximum of 32 to prevent under/over-utilization.
func calculateOptimalWorkers() int {
	cores := runtime.NumCPU()
	// Use 75% of cores, minimum 4, maximum 32
	workers := cores * 3 / 4
	if workers < 4 {
		workers = 4
	}
	if workers > 32 {
		workers = 32
	}
	return workers
}

type StateSyncerConfig struct {
	Root                     common.Hash
	Client                   syncclient.Client
	DB                       ethdb.Database
	BatchSize                int
	MaxOutstandingCodeHashes int    // Maximum number of code hashes in the code syncer queue
	NumCodeFetchingWorkers   int    // Number of code syncing threads
	NumWorkers               int    // Number of concurrent sync workers (0 = auto-calculate based on CPU cores)
	RequestSize              uint16 // Number of leafs to request from a peer at a time
}

// stateSync keeps the state of the entire state sync operation.
type stateSync struct {
	db        ethdb.Database            // database we are syncing
	root      common.Hash               // root of the EVM state we are syncing to
	trieDB    *triedb.Database          // trieDB on top of db we are syncing. used to restore any existing tries.
	snapshot  snapshot.SnapshotIterable // used to access the database we are syncing as a snapshot.
	batchSize int                       // write batches when they reach this size
	numWorkers int                      // number of concurrent sync workers

	// Config values needed for Restart()
	client                   syncclient.Client
	requestSize              uint16
	maxOutstandingCodeHashes int
	numCodeFetchingWorkers   int

	segments   chan syncclient.LeafSyncTask   // channel of tasks to sync
	syncer     *syncclient.CallbackLeafSyncer // performs the sync, looping over each task's range and invoking specified callbacks
	codeSyncer *codeSyncer                    // manages the asynchronous download and batching of code hashes
	trieQueue  *trieQueue                     // manages a persistent list of storage tries we need to sync and any segments that are created for them

	// track the main account trie specifically to commit its root at the end of the operation
	mainTrie *trieToSync

	// track the tries currently being synced
	lock            sync.RWMutex
	triesInProgress map[common.Hash]*trieToSync

	// Mutex to protect Start() and Restart() from running concurrently
	// This prevents race conditions between manual state sync and reviver-triggered restarts
	syncMutex sync.Mutex

	// track completion and progress of work
	mainTrieDone       chan struct{}
	storageTriesDone   chan struct{}
	triesInProgressSem chan struct{}
	done               chan error
	stats              *trieSyncStats
	stuckDetector      *StuckDetector // monitors for stalled sync and triggers fallback

	// context cancellation management
	cancelFunc            context.CancelFunc
	started               atomic.Bool    // prevents multiple Start() calls
	waitStarted           atomic.Bool    // prevents multiple Wait() calls
	cachedResult          atomic.Value   // stores *error from first Wait() call for reuse
	resultReady           chan struct{}  // closed when cachedResult is available
	mainTrieDoneOnce      sync.Once      // ensures mainTrieDone channel is closed only once
	segmentsDoneOnce      sync.Once      // ensures segments channel is closed only once
	storageTriesDoneOnce  sync.Once      // ensures storageTriesDone channel is closed only once

	// Code sync error tracking for reviver retry
	// Allows storage tries to complete even when code sync fails
	codeSyncErr    atomic.Value // stores *error from code sync
	codeSyncFailed atomic.Bool  // true if code sync failed but storage completed
}

func NewStateSyncer(config *StateSyncerConfig) (*stateSync, error) {
	// Validate configuration
	if config.RequestSize < minRequestSize || config.RequestSize > maxRequestSize {
		return nil, fmt.Errorf("%w: got %d", errInvalidRequestSize, config.RequestSize)
	}
	if config.NumCodeFetchingWorkers <= 0 {
		return nil, errInvalidWorkerConfig
	}

	// Determine number of workers: use config value if specified, otherwise calculate based on CPU cores
	numWorkers := config.NumWorkers
	if numWorkers <= 0 {
		numWorkers = calculateOptimalWorkers()
		log.Info("Auto-calculated optimal worker count", "workers", numWorkers, "cpuCores", runtime.NumCPU())
	} else {
		// Validate configured worker count to prevent excessive resource allocation
		if numWorkers > 128 {
			log.Warn("Configured worker count exceeds recommended maximum, capping at 128",
				"configured", numWorkers, "capped", 128)
			numWorkers = 128
		}
		log.Info("Using configured worker count", "workers", numWorkers)
	}

	// CRITICAL: Enforce minimum worker count to prevent deadlock
	// With numWorkers < 2, if the main trie holds the semaphore, storage tries can't be processed
	// This causes a deadlock where main trie waits for storage tries, but storage tries can't start
	if numWorkers < 2 {
		log.Warn("Worker count too low, enforcing minimum of 2 to prevent deadlock",
			"configured", numWorkers, "enforced", 2)
		numWorkers = 2
	}

	ss := &stateSync{
		batchSize:       config.BatchSize,
		db:              config.DB,
		root:            config.Root,
		trieDB:          triedb.NewDatabase(config.DB, nil),
		snapshot:        snapshot.NewDiskLayer(config.DB),
		stats:           newTrieSyncStats(),
		triesInProgress: make(map[common.Hash]*trieToSync),
		numWorkers:      numWorkers,

		// Store config values for Restart()
		client:                   config.Client,
		requestSize:              config.RequestSize,
		maxOutstandingCodeHashes: config.MaxOutstandingCodeHashes,
		numCodeFetchingWorkers:   config.NumCodeFetchingWorkers,

		// [triesInProgressSem] is used to keep the number of tries syncing
		// less than or equal to [numWorkers].
		triesInProgressSem: make(chan struct{}, numWorkers),

		// Each [trieToSync] will have a maximum of [numSegments] segments.
		// We set the capacity of [segments] such that [numWorkers]
		// storage tries can sync concurrently.
		segments:         make(chan syncclient.LeafSyncTask, numWorkers*numStorageTrieSegments),
		mainTrieDone:     make(chan struct{}),
		storageTriesDone: make(chan struct{}),
		done:             make(chan error, 1),
		resultReady:      make(chan struct{}),
	}

	// Initialize stuck detector to monitor sync progress
	ss.stuckDetector = NewStuckDetector(ss.stats)
	if ss.stuckDetector == nil {
		return nil, errors.New("failed to create stuck detector")
	}
	log.Info("Stuck detector created successfully")

	ss.syncer = syncclient.NewCallbackLeafSyncer(config.Client, ss.segments, config.RequestSize)
	ss.codeSyncer = newCodeSyncer(CodeSyncerConfig{
		DB:                       config.DB,
		Client:                   config.Client,
		MaxOutstandingCodeHashes: config.MaxOutstandingCodeHashes,
		NumCodeFetchingWorkers:   config.NumCodeFetchingWorkers,
	})

	ss.trieQueue = NewTrieQueue(config.DB)
	if err := ss.trieQueue.clearIfRootDoesNotMatch(ss.root); err != nil {
		return nil, err
	}

	// create a trieToSync for the main trie and mark it as in progress.
	var err error
	ss.mainTrie, err = NewTrieToSync(ss, ss.root, common.Hash{}, NewMainTrieTask(ss))
	if err != nil {
		return nil, err
	}
	ss.addTrieInProgress(ss.root, ss.mainTrie)
	ss.mainTrie.startSyncing() // start syncing after tracking the trie as in progress
	return ss, nil
}

// onStorageTrieFinished is called after a storage trie finishes syncing.
func (t *stateSync) onStorageTrieFinished(root common.Hash) error {
	<-t.triesInProgressSem // allow another trie to start (release the semaphore)
	// mark the storage trie as done in trieQueue
	if err := t.trieQueue.StorageTrieDone(root); err != nil {
		return err
	}
	// track the completion of this storage trie
	numInProgress, err := t.removeTrieInProgress(root)
	if err != nil {
		return err
	}
	if numInProgress == 0 {
		select {
		case <-t.storageTriesDone:
			// when the last storage trie finishes, close the segments channel (safe to call multiple times)
			t.segmentsDoneOnce.Do(func() {
				close(t.segments)
			})
		default:
		}
	}
	return nil
}

// onMainTrieFinished is called after the main trie finishes syncing.
func (t *stateSync) onMainTrieFinished() error {
	t.codeSyncer.notifyAccountTrieCompleted()

	// count the number of storage tries we need to sync for eta purposes.
	numStorageTries, err := t.trieQueue.countTries()
	if err != nil {
		return err
	}
	t.stats.setTriesRemaining(numStorageTries)

	// mark the main trie done (safe to call multiple times)
	t.mainTrieDoneOnce.Do(func() {
		close(t.mainTrieDone)
	})
	_, err = t.removeTrieInProgress(t.root)
	return err
}

// onSyncComplete is called after the account trie and
// all storage tries have completed syncing. We persist
// [mainTrie]'s batch last to avoid persisting the state
// root before all storage tries are done syncing.
func (t *stateSync) onSyncComplete() error {
	return t.mainTrie.batch.Write()
}

// storageTrieProducer waits for the main trie to finish
// syncing then starts to add storage trie roots along
// with their corresponding accounts to the segments channel.
// returns nil if all storage tries were iterated and an
// error if one occurred or the context expired.
func (t *stateSync) storageTrieProducer(ctx context.Context) error {
	// Wait for main trie to finish to ensure when this thread terminates
	// there are no more storage tries to sync
	select {
	case <-t.mainTrieDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	for {
		// check ctx here to exit the loop early
		if err := ctx.Err(); err != nil {
			return err
		}

		root, accounts, more, err := t.trieQueue.getNextTrie()
		if err != nil {
			return err
		}
		// If there are no storage tries, then root will be the empty hash on the first pass.
		if root == (common.Hash{}) && !more {
			t.segmentsDoneOnce.Do(func() {
				close(t.segments)
			})
			return nil
		}

		// acquire semaphore (to keep number of tries in progress limited)
		select {
		case t.triesInProgressSem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		// Arbitrarily use the first account for making requests to the server.
		// Note: getNextTrie guarantees that if a non-nil storage root is returned, then the
		// slice of account hashes is non-empty.
		syncAccount := accounts[0]

		// create a trieToSync for the storage trie and mark it as in progress.
		storageTrie, err := NewTrieToSync(t, root, syncAccount, NewStorageTrieTask(t, root, accounts))
		if err != nil {
			return err
		}
		t.addTrieInProgress(root, storageTrie)
		if !more {
			t.storageTriesDoneOnce.Do(func() {
				close(t.storageTriesDone)
			})
		}
		// start syncing after tracking the trie as in progress
		storageTrie.startSyncing()

		if !more {
			return nil
		}
	}
}

// ErrorCategory classifies state sync errors
type ErrorCategory int

const (
	ErrorCategoryTransient ErrorCategory = iota // Temporary network/peer issues
	ErrorCategoryRetryable                      // Peer unavailability, worth retrying (preserve state for reviver)
	ErrorCategoryFatal                          // Unrecoverable (DB corruption, invalid state)
	ErrorCategoryStuck                          // Progress stalled, needs fallback
)

// categorizeStateSyncError classifies errors to determine appropriate action
func categorizeStateSyncError(err error) ErrorCategory {
	if err == nil {
		return ErrorCategoryTransient
	}

	errStr := err.Error()

	// Stuck detection (explicit)
	if errors.Is(err, errStateSyncStuck) {
		return ErrorCategoryStuck
	}

	// Fatal errors (don't fallback, require intervention)
	if strings.Contains(errStr, "database") && strings.Contains(errStr, "corrupt") {
		return ErrorCategoryFatal
	}

	if strings.Contains(errStr, "panic") {
		return ErrorCategoryFatal
	}

	// IMPORTANT: Distinguish code sync failures from other retry exhaustion
	// Code sync failures are peer-related (peers don't have data), not stuck
	// Check for code sync specific error patterns first, before general retry exhaustion
	if strings.Contains(errStr, "code request") ||
		strings.Contains(errStr, "CodeRequest") ||
		strings.Contains(errStr, "code sync") ||
		strings.Contains(errStr, "code hashes") {

		// Code sync failures with retry exhaustion are retryable (not stuck)
		if strings.Contains(errStr, "too many retries") ||
			strings.Contains(errStr, "retry") {
			log.Warn("Code sync retry exhaustion - preserving state for reviver",
				"error", errStr)
			return ErrorCategoryRetryable
		}

		// Other code sync errors (timeouts, peer unavailable, etc.) are also retryable
		log.Warn("Code sync error detected - preserving state for reviver",
			"error", errStr)
		return ErrorCategoryRetryable
	}

	// Other retry exhaustion (trie segments, etc.) is stuck, not retryable
	if strings.Contains(errStr, "too many retries") {
		return ErrorCategoryStuck
	}

	// Other stuck indicators
	if strings.Contains(errStr, "no leafs fetched") ||
		strings.Contains(errStr, "no trie completed") {
		return ErrorCategoryStuck
	}

	// Context cancellation is transient
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorCategoryTransient
	}

	// Everything else is transient
	return ErrorCategoryTransient
}

// shouldFallbackToBlockSync determines if we should fallback based on error
func shouldFallbackToBlockSync(err error) bool {
	category := categorizeStateSyncError(err)
	// Fallback for both Stuck and Retryable errors
	// The key difference: Retryable preserves summary for reviver, Stuck may not
	return category == ErrorCategoryStuck || category == ErrorCategoryRetryable
}

func (t *stateSync) Start(ctx context.Context) error {
	// CRITICAL: Acquire syncMutex to prevent concurrent Start()/Restart() calls
	// This ensures mutual exclusion between manual state sync and reviver retries
	t.syncMutex.Lock()
	defer t.syncMutex.Unlock()

	// Prevent multiple Start() calls
	if !t.started.CompareAndSwap(false, true) {
		return errors.New("state sync already started")
	}

	return t.doStart(ctx)
}

// doStart contains the core Start() logic without mutex acquisition.
// This method should only be called while holding syncMutex.
// Separated to allow Restart() to call it without causing deadlock.
func (t *stateSync) doStart(ctx context.Context) error {
	// Create a cancellable context for the sync operations
	syncCtx, cancel := context.WithCancel(ctx)
	t.cancelFunc = cancel

	// Start stuck detector to monitor for stalled sync
	log.Info("Starting stuck detector for state sync monitoring")
	t.stuckDetector.Start(syncCtx)

	// Start the code syncer and leaf syncer.
	eg, egCtx := errgroup.WithContext(syncCtx)
	t.codeSyncer.start(egCtx) // start the code syncer first since the leaf syncer may add code tasks
	t.syncer.Start(egCtx, t.numWorkers, t.onSyncFailure)

	// Wrap all goroutines with panic recovery
	// CRITICAL: recover() must be called directly in defer, not in a helper function
	eg.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("PANIC in syncer.Done goroutine, triggering fallback",
					"panic", r,
					"stack", string(debug.Stack()))
				cancel() // Cancel other goroutines
				err = fmt.Errorf("panic in syncer.Done: %v", r)
			}
		}()
		if syncErr := <-t.syncer.Done(); syncErr != nil {
			return syncErr
		}
		return t.onSyncComplete()
	})

	eg.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("PANIC in codeSyncer.Done goroutine",
					"panic", r,
					"stack", string(debug.Stack()))
				// Don't cancel - let storage tries finish, but still fail the sync
				// Panics are programming bugs, not "code unavailable" scenarios
				err = fmt.Errorf("panic in codeSyncer.Done: %v", r)
			}
		}()

		codeSyncErr := <-t.codeSyncer.Done()
		if codeSyncErr != nil {
			// Context cancelled is expected during shutdown
			if errors.Is(codeSyncErr, context.Canceled) {
				return codeSyncErr
			}

			// Code sync failure is NOT fatal - storage tries don't need code to complete
			// Storage tries only contain key-value pairs, code can be synced later
			log.Warn("Code sync failed but allowing storage tries to complete",
				"err", codeSyncErr,
				"triesSynced", t.stats.getProgress())

			// Store the error for potential retry by reviver
			t.storeCodeSyncError(codeSyncErr)
			return nil // Don't fail entire sync
		}

		log.Info("Code sync completed successfully")
		return nil
	})

	eg.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("PANIC in storageTrieProducer goroutine, triggering fallback",
					"panic", r,
					"stack", string(debug.Stack()))
				cancel() // Cancel other goroutines
				err = fmt.Errorf("panic in storageTrieProducer: %v", r)
			}
		}()
		return t.storageTrieProducer(egCtx)
	})

	// Monitor for stuck detection alongside sync operations
	eg.Go(func() error {
		select {
		case <-t.stuckDetector.StuckChannel():
			// Log final state before canceling
			triesSynced, triesRemaining := t.stats.getProgress()
			log.Warn("Stuck detected, canceling state sync to trigger fallback",
				"triesSynced", triesSynced,
				"triesRemaining", triesRemaining,
				"totalLeafs", t.stats.totalLeafs.Snapshot().Count())
			cancel() // Cancel sync to trigger fallback
			return errStateSyncStuck
		case <-egCtx.Done():
			return nil // Normal completion or cancellation
		}
	})

	// Monitor for code sync phase to enable faster stuck detection
	eg.Go(func() error {
		// Wait for main trie and storage tries to complete
		select {
		case <-t.mainTrieDone:
			// Main trie done, now wait for storage tries
			select {
			case <-t.storageTriesDone:
				// Both done, now primarily in code sync phase
				t.stuckDetector.EnterCodeSyncPhase()
				// Notify that storage tries are waiting for code sync to complete
				// This enables faster stuck detection if code sync fails
				t.stuckDetector.NotifyStorageTriesWaitingForCode()
			case <-egCtx.Done():
				return nil
			}
		case <-egCtx.Done():
			return nil
		}

		// Wait for completion or cancellation
		<-egCtx.Done()
		t.stuckDetector.ExitCodeSyncPhase()
		t.stuckDetector.ClearStorageTriesWaiting()
		return nil
	})

	// The errgroup wait will take care of returning the first error that occurs, or returning
	// nil if all finish without an error.
	go func() {
		err := eg.Wait()
		t.stuckDetector.Stop() // Stop monitoring when sync completes or fails

		// Categorize error and trigger appropriate fallback
		if err != nil && !errors.Is(err, context.Canceled) {
			if shouldFallbackToBlockSync(err) {
				log.Warn("State sync error requires fallback to block sync",
					"err", err,
					"errType", categorizeStateSyncError(err))
				err = errStateSyncStuck
			}
		}

		t.done <- err
	}()
	return nil
}

func (t *stateSync) Wait(ctx context.Context) error {
	// This should only be called after Start, so we can assume cancelFunc is set.
	if t.cancelFunc == nil {
		return errWaitBeforeStart
	}

	// Check if Wait() was already called - return cached result
	if !t.waitStarted.CompareAndSwap(false, true) {
		// Another goroutine already called Wait() - wait for result to be ready
		<-t.resultReady // Block until first Wait() completes and closes this channel

		// Now safe to load cached result
		result := t.cachedResult.Load()
		if result == nil {
			return errors.New("internal error: resultReady closed but cachedResult is nil")
		}

		// Safe type assertion with comma-ok pattern
		errPtr, ok := result.(*error)
		if !ok {
			return errors.New("internal error: cachedResult has wrong type")
		}
		return *errPtr
	}

	var resultErr error
	select {
	case err := <-t.done:
		resultErr = err
	case <-ctx.Done():
		t.cancelFunc() // cancel the sync operations if the context is done
		<-t.done       // wait for the sync operations to finish
		resultErr = ctx.Err()
	}

	// Cache the result for future Wait() calls and signal they can proceed
	t.cachedResult.Store(&resultErr)
	close(t.resultReady) // Signal that result is now available
	return resultErr
}

// Restart reinitializes the state syncer for a retry attempt after fallback.
// Called by the reviver mechanism when automatically retrying failed state sync.
// The startReqID ensures message IDs don't conflict with previous attempts.
//
// IMPORTANT: Only call after Start() has completed and Wait() has returned.
// The concurrency guards (sync.Once, cachedResult) prevent issues from multiple calls.
// This method recreates all channels, workers, and internal state for a fresh start.
func (t *stateSync) Restart(ctx context.Context, startReqID uint32) error {
	// CRITICAL: Acquire syncMutex to prevent concurrent Start()/Restart() calls
	// This ensures mutual exclusion between manual state sync and reviver retries
	t.syncMutex.Lock()
	defer t.syncMutex.Unlock()

	// Check if a sync is currently running
	if t.started.Load() {
		return errors.New("cannot restart: state sync already running")
	}

	// CRITICAL: Verify Wait() was called before Restart()
	// Restart() requires that the previous sync completed and Wait() returned
	// Otherwise we could have abandoned channels with blocked goroutines
	if !t.waitStarted.Load() {
		return errors.New("cannot restart: Wait() must be called before Restart()")
	}

	log.Warn("===========================================")
	log.Warn("STATE SYNC RESTART REQUESTED BY REVIVER")
	log.Warn("===========================================",
		"startReqID", startReqID,
		"root", t.root.Hex())

	// Reset atomic state flags
	t.started.Store(false)
	t.waitStarted.Store(false)

	// CRITICAL: Cancel old context to stop any lingering goroutines
	// This prevents goroutine leaks from failed previous attempts
	if t.cancelFunc != nil {
		log.Info("Cancelling previous state sync context to cleanup goroutines")
		t.cancelFunc() // Cancel old context first

		// Wait for previous sync to fully cleanup
		// The done channel will be closed when all goroutines exit
		select {
		case <-t.done:
			log.Info("Previous state sync cleanup completed")
		case <-time.After(10 * time.Second):
			log.Warn("Timeout waiting for previous sync cleanup, proceeding anyway")
		}
	}
	t.cancelFunc = nil

	// Recreate all channels (old ones are closed)
	t.done = make(chan error, 1)
	t.resultReady = make(chan struct{})
	t.mainTrieDone = make(chan struct{})
	t.storageTriesDone = make(chan struct{})

	// CRITICAL: Recreate segments channel (old one was closed)
	// Must have same capacity as original (numWorkers * numStorageTrieSegments)
	t.segments = make(chan syncclient.LeafSyncTask, t.numWorkers*numStorageTrieSegments)

	// CRITICAL: Recreate triesInProgressSem (semaphore may be exhausted from previous run)
	t.triesInProgressSem = make(chan struct{}, t.numWorkers)

	// Reset the Once guards so channels can be closed again
	t.mainTrieDoneOnce = sync.Once{}
	t.segmentsDoneOnce = sync.Once{}
	t.storageTriesDoneOnce = sync.Once{}

	// CRITICAL: Recreate syncer with new segments channel
	// The old syncer holds a reference to the closed segments channel
	t.syncer = syncclient.NewCallbackLeafSyncer(t.client, t.segments, t.requestSize)

	// CRITICAL: Recreate codeSyncer to reset its internal state
	// The old codeSyncer may have stale code hashes or error state
	t.codeSyncer = newCodeSyncer(CodeSyncerConfig{
		DB:                       t.db,
		Client:                   t.client,
		MaxOutstandingCodeHashes: t.maxOutstandingCodeHashes,
		NumCodeFetchingWorkers:   t.numCodeFetchingWorkers,
	})

	// Clear triesInProgress map (stale references from previous run)
	t.lock.Lock()
	t.triesInProgress = make(map[common.Hash]*trieToSync)
	t.lock.Unlock()

	// Reset stats for fresh monitoring of this retry attempt
	// Stuck detector relies on rate calculations, so we need fresh stats
	t.stats = newTrieSyncStats()

	// Verify trieQueue root matches (should match because we preserved summary)
	if err := t.trieQueue.clearIfRootDoesNotMatch(t.root); err != nil {
		log.Error("REVIVER: trieQueue root mismatch during restart", "err", err)
		return fmt.Errorf("trieQueue root mismatch: %w", err)
	}

	// CRITICAL: Recreate mainTrie (old one has stale state)
	var err error
	t.mainTrie, err = NewTrieToSync(t, t.root, common.Hash{}, NewMainTrieTask(t))
	if err != nil {
		log.Error("REVIVER: Failed to create mainTrie during restart", "err", err)
		return fmt.Errorf("failed to create mainTrie: %w", err)
	}

	// Track mainTrie as in progress and start syncing
	t.addTrieInProgress(t.root, t.mainTrie)
	t.mainTrie.startSyncing()

	// Reset stuck detector to monitor the new sync attempt (uses fresh stats)
	t.stuckDetector = NewStuckDetector(t.stats)

	log.Info("State sync restart initialized, calling doStart()...")

	// Start the sync again - this will use the preserved summary
	// CRITICAL: Call doStart() directly (not Start()) to avoid deadlock
	// We already hold syncMutex, and Start() would try to acquire it again
	err = t.doStart(ctx)
	if err != nil {
		log.Error("REVIVER: Restart failed during doStart()",
			"err", err)
		return fmt.Errorf("restart start failed: %w", err)
	}

	log.Warn("REVIVER: State sync restart initiated successfully")
	return nil
}

// addTrieInProgress tracks the root as being currently synced.
func (t *stateSync) addTrieInProgress(root common.Hash, trie *trieToSync) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.triesInProgress[root] = trie
}

// removeTrieInProgress removes root from the set of tracked tries in progress
// and returns the number of tries in progress after the removal.
func (t *stateSync) removeTrieInProgress(root common.Hash) (int, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.stats.trieDone(root)
	if _, ok := t.triesInProgress[root]; !ok {
		return 0, fmt.Errorf("removeTrieInProgress for unexpected root: %s", root)
	}
	delete(t.triesInProgress, root)
	return len(t.triesInProgress), nil
}

// onSyncFailure is called if the sync fails, this writes all
// batches of in-progress trie segments to disk to have maximum
// progress to restore.
func (t *stateSync) onSyncFailure(error) error {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for _, trie := range t.triesInProgress {
		for _, segment := range trie.segments {
			if err := segment.batch.Write(); err != nil {
				return err
			}
		}
	}
	return nil
}

// storeCodeSyncError stores a code sync error for potential retry by the reviver.
// This allows storage tries to complete even when code sync fails, with the error
// preserved for a subsequent retry attempt.
func (t *stateSync) storeCodeSyncError(err error) {
	if err != nil {
		// Store error first, then set flag (ordering matters for concurrent access)
		t.codeSyncErr.Store(err)
		t.codeSyncFailed.Store(true)
		log.Warn("Code sync error stored for potential reviver retry", "err", err)
	}
}

// getCodeSyncError retrieves the stored code sync error, if any.
// Returns nil if no code sync error was stored.
func (t *stateSync) getCodeSyncError() error {
	if !t.codeSyncFailed.Load() {
		return nil
	}
	errVal := t.codeSyncErr.Load()
	if errVal == nil {
		return nil
	}
	return errVal.(error)
}
