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

	segments   chan syncclient.LeafSyncTask   // channel of tasks to sync
	syncer     *syncclient.CallbackLeafSyncer // performs the sync, looping over each task's range and invoking specified callbacks
	codeSyncer *codeSyncer                    // manages the asynchronous download and batching of code hashes
	trieQueue  *trieQueue                     // manages a persistent list of storage tries we need to sync and any segments that are created for them

	// track the main account trie specifically to commit its root at the end of the operation
	mainTrie *trieToSync

	// track the tries currently being synced
	lock            sync.RWMutex
	triesInProgress map[common.Hash]*trieToSync

	// track completion and progress of work
	mainTrieDone       chan struct{}
	storageTriesDone   chan struct{}
	triesInProgressSem chan struct{}
	done               chan error
	stats              *trieSyncStats
	stuckDetector      *StuckDetector // monitors for stalled sync and triggers fallback

	// context cancellation management
	cancelFunc context.CancelFunc
	started    atomic.Bool // prevents multiple Start() calls
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

	ss := &stateSync{
		batchSize:       config.BatchSize,
		db:              config.DB,
		root:            config.Root,
		trieDB:          triedb.NewDatabase(config.DB, nil),
		snapshot:        snapshot.NewDiskLayer(config.DB),
		stats:           newTrieSyncStats(),
		triesInProgress: make(map[common.Hash]*trieToSync),
		numWorkers:      numWorkers,

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
			// when the last storage trie finishes, close the segments channel
			close(t.segments)
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

	// mark the main trie done
	close(t.mainTrieDone)
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
			close(t.segments)
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
			close(t.storageTriesDone)
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
	ErrorCategoryFatal                          // Unrecoverable (DB corruption, invalid state)
	ErrorCategoryStuck                          // Progress stalled, needs fallback
)

// categorizeStateSyncError classifies errors to determine appropriate action
func categorizeStateSyncError(err error) ErrorCategory {
	if err == nil {
		return ErrorCategoryTransient
	}

	errStr := err.Error()

	// Stuck detection
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

	// Stuck indicators
	if strings.Contains(errStr, "too many retries") ||
		strings.Contains(errStr, "no leafs fetched") ||
		strings.Contains(errStr, "no trie completed") {
		return ErrorCategoryStuck
	}

	// Everything else is transient
	return ErrorCategoryTransient
}

// shouldFallbackToBlockSync determines if we should fallback based on error
func shouldFallbackToBlockSync(err error) bool {
	category := categorizeStateSyncError(err)
	return category == ErrorCategoryStuck
}

func (t *stateSync) Start(ctx context.Context) error {
	// Prevent multiple Start() calls
	if !t.started.CompareAndSwap(false, true) {
		return errors.New("state sync already started")
	}

	// Create a cancellable context for the sync operations
	syncCtx, cancel := context.WithCancel(ctx)
	t.cancelFunc = cancel

	// Start stuck detector to monitor for stalled sync
	log.Info("Starting stuck detector for state sync monitoring")
	t.stuckDetector.Start(syncCtx)

	// Panic recovery wrapper - returns error from panic to errgroup
	panicRecovery := func(name string) error {
		if r := recover(); r != nil {
			log.Error("PANIC in state sync goroutine, triggering fallback",
				"goroutine", name,
				"panic", r,
				"stack", string(debug.Stack()))
			cancel() // Cancel other goroutines
			return fmt.Errorf("panic in %s: %v", name, r)
		}
		return nil
	}

	// Start the code syncer and leaf syncer.
	eg, egCtx := errgroup.WithContext(syncCtx)
	t.codeSyncer.start(egCtx) // start the code syncer first since the leaf syncer may add code tasks
	t.syncer.Start(egCtx, t.numWorkers, t.onSyncFailure)

	// Wrap all goroutines with panic recovery
	eg.Go(func() (err error) {
		defer func() {
			if panicErr := panicRecovery("syncer.Done"); panicErr != nil {
				err = panicErr
			}
		}()
		if err := <-t.syncer.Done(); err != nil {
			return err
		}
		return t.onSyncComplete()
	})

	eg.Go(func() (err error) {
		defer func() {
			if panicErr := panicRecovery("codeSyncer.Done"); panicErr != nil {
				err = panicErr
			}
		}()
		return <-t.codeSyncer.Done()
	})

	eg.Go(func() (err error) {
		defer func() {
			if panicErr := panicRecovery("storageTrieProducer"); panicErr != nil {
				err = panicErr
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

	select {
	case err := <-t.done:
		return err
	case <-ctx.Done():
		t.cancelFunc() // cancel the sync operations if the context is done
		<-t.done       // wait for the sync operations to finish
		return ctx.Err()
	}
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
