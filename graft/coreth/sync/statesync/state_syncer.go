// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/triedb"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
	syncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
)

const (
	// Segment threshold constants for adaptive sizing
	baseSegmentThreshold = 500_000   // Default threshold for medium-memory systems
	lowMemThreshold      = 250_000   // Conservative threshold for <8GB systems
	highMemThreshold     = 1_000_000 // Aggressive threshold for >=16GB systems

	// Memory tier thresholds in GB
	lowMemoryGB  = 8
	highMemoryGB = 16

	numStorageTrieSegments = 4
	numMainTrieSegments    = 8
	defaultNumWorkers      = 12 // Tested working value - balances performance vs memory usage
)

var (
	_                           syncpkg.Syncer = (*stateSync)(nil)
	errCodeRequestQueueRequired                = errors.New("code request queue is required")
	errLeafsRequestSizeRequired                = errors.New("leafs request size must be > 0")

	// Cache the computed segment threshold to avoid repeated memory stats calls
	cachedSegmentThreshold     uint64
	segmentThresholdComputedOnce sync.Once
)

// getSegmentThreshold returns the adaptive segment threshold based on available system memory.
// Caches the result since system memory doesn't change during runtime.
func getSegmentThreshold() uint64 {
	segmentThresholdComputedOnce.Do(func() {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		// Calculate total system memory (approximated from heap limit)
		// Sys includes all memory obtained from OS, giving us a reasonable estimate
		totalMemoryGB := float64(memStats.Sys) / (1024 * 1024 * 1024)

		switch {
		case totalMemoryGB < lowMemoryGB:
			// Conservative for low-memory nodes
			cachedSegmentThreshold = lowMemThreshold
			log.Info("Using conservative segment threshold for low-memory system",
				"threshold", cachedSegmentThreshold, "memoryGB", totalMemoryGB)
		case totalMemoryGB >= highMemoryGB:
			// Aggressive for high-memory nodes
			cachedSegmentThreshold = highMemThreshold
			log.Info("Using aggressive segment threshold for high-memory system",
				"threshold", cachedSegmentThreshold, "memoryGB", totalMemoryGB)
		default:
			// Default for medium-memory nodes
			cachedSegmentThreshold = baseSegmentThreshold
			log.Info("Using default segment threshold for medium-memory system",
				"threshold", cachedSegmentThreshold, "memoryGB", totalMemoryGB)
		}
	})
	return cachedSegmentThreshold
}

// stateSync keeps the state of the entire state sync operation.
type stateSync struct {
	db        ethdb.Database                 // database we are syncing
	root      common.Hash                    // root of the EVM state we are syncing to
	trieDB    *triedb.Database               // trieDB on top of db we are syncing. used to restore any existing tries.
	snapshot  snapshot.SnapshotIterable      // used to access the database we are syncing as a snapshot.
	batchSize uint                           // write batches when they reach this size
	segments  chan syncclient.LeafSyncTask   // channel of tasks to sync
	syncer    *syncclient.CallbackLeafSyncer // performs the sync, looping over each task's range and invoking specified callbacks
	codeQueue *CodeQueue                     // queue that manages the asynchronous download and batching of code hashes
	trieQueue *trieQueue                     // manages a persistent list of storage tries we need to sync and any segments that are created for them

	// track the main account trie specifically to commit its root at the end of the operation
	mainTrie *trieToSync

	// track the tries currently being synced
	lock            sync.RWMutex
	triesInProgress map[common.Hash]*trieToSync

	// track completion and progress of work
	mainTrieDone       chan struct{}
	storageTriesDone   chan struct{}
	triesInProgressSem chan struct{}
	stats              *trieSyncStats

	// syncCompleted is set to true when the sync completes successfully.
	// This provides an explicit success signal for Finalize().
	syncCompleted atomic.Bool
}

// SyncerOption configures the state syncer via functional options.
type SyncerOption = options.Option[stateSync]

// WithBatchSize sets the database batch size for writes.
func WithBatchSize(n uint) SyncerOption {
	return options.Func[stateSync](func(s *stateSync) {
		if n > 0 {
			s.batchSize = n
		}
	})
}

func NewSyncer(client syncclient.Client, db ethdb.Database, root common.Hash, codeQueue *CodeQueue, leafsRequestSize uint16, opts ...SyncerOption) (syncpkg.Syncer, error) {
	if leafsRequestSize == 0 {
		return nil, errLeafsRequestSizeRequired
	}

	// Construct with defaults, then apply options directly to stateSync.
	ss := &stateSync{
		db:              db,
		root:            root,
		trieDB:          triedb.NewDatabase(db, nil),
		snapshot:        snapshot.NewDiskLayer(db),
		stats:           newTrieSyncStats(),
		triesInProgress: make(map[common.Hash]*trieToSync),

		// [triesInProgressSem] is used to keep the number of tries syncing
		// less than or equal to [defaultNumWorkers].
		triesInProgressSem: make(chan struct{}, defaultNumWorkers),

		// Each [trieToSync] will have a maximum of [numSegments] segments.
		// We set the capacity of [segments] to accommodate the main trie (8 segments)
		// plus concurrent storage tries. Sized for main trie to avoid blocking during initialization.
		segments:         make(chan syncclient.LeafSyncTask, defaultNumWorkers*numMainTrieSegments),
		mainTrieDone:     make(chan struct{}),
		storageTriesDone: make(chan struct{}),
		batchSize:        ethdb.IdealBatchSize * 4, // 4x larger batches = fewer DB writes (I/O optimization)
	}

	// Apply functional options.
	options.ApplyTo(ss, opts...)

	ss.syncer = syncclient.NewCallbackLeafSyncer(client, ss.segments, &syncclient.LeafSyncerConfig{
		RequestSize: leafsRequestSize,
		NumWorkers:  defaultNumWorkers,
	})

	if codeQueue == nil {
		return nil, errCodeRequestQueueRequired
	}

	ss.codeQueue = codeQueue

	ss.trieQueue = NewTrieQueue(db)
	if err := ss.trieQueue.clearIfRootDoesNotMatch(ss.root); err != nil {
		return nil, err
	}

	var err error
	// create a trieToSync for the main trie and mark it as in progress.
	ss.mainTrie, err = NewTrieToSync(ss, ss.root, common.Hash{}, NewMainTrieTask(ss))
	if err != nil {
		return nil, err
	}
	ss.addTrieInProgress(ss.root, ss.mainTrie)

	// Use context.Background() for initialization since we don't have a sync context yet.
	// This is safe because startSyncing is called before Sync() starts.
	if err := ss.mainTrie.startSyncing(context.Background()); err != nil {
		return nil, err
	}
	return ss, nil
}

// Name returns the human-readable name for this sync task.
func (*stateSync) Name() string {
	return "EVM State Syncer"
}

// ID returns the stable identifier for this sync task.
func (*stateSync) ID() string {
	return "state_evm_state_sync"
}

func (t *stateSync) Sync(ctx context.Context) error {
	log.Info("[DEBUG-SYNC] State sync starting",
		"root", t.root,
		"numWorkers", defaultNumWorkers,
	)

	// Start the leaf syncer and storage trie producer.
	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				log.Error("[PANIC-SYNC] Leaf syncer panicked", "panic", r, "stack", fmt.Sprintf("%+v", r))
				panic(r)
			}
		}()
		log.Info("[DEBUG-SYNC] Leaf syncer goroutine started")
		if err := t.syncer.Sync(egCtx); err != nil {
			log.Error("[ERROR-SYNC] Leaf syncer failed", "err", err)
			return err
		}
		log.Info("[DEBUG-SYNC] Leaf syncer completed, calling onSyncComplete")
		return t.onSyncComplete()
	})

	// Note: code fetcher should already be initialized.
	eg.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				log.Error("[PANIC-SYNC] Storage trie producer panicked", "panic", r, "stack", fmt.Sprintf("%+v", r))
				panic(r)
			}
		}()
		log.Info("[DEBUG-SYNC] Storage trie producer goroutine started")
		return t.storageTrieProducer(egCtx)
	})

	// The errgroup wait will take care of returning the first error that occurs, or returning
	// nil if syncing finish without an error.
	log.Info("[DEBUG-SYNC] Waiting for sync goroutines")
	err := eg.Wait()
	if err != nil {
		log.Error("[ERROR-SYNC] State sync failed", "err", err)
	} else {
		log.Info("[DEBUG-SYNC] State sync completed successfully")
	}
	return err
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
	if err := t.codeQueue.Finalize(); err != nil {
		return err
	}

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
	if err := t.mainTrie.batch.Write(); err != nil {
		return err
	}
	t.syncCompleted.Store(true)
	return nil
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
		if err := storageTrie.startSyncing(ctx); err != nil {
			return err
		}

		if !more {
			return nil
		}
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

// Finalize flushes in-progress trie batches to disk to preserve progress on failure.
func (t *stateSync) Finalize() error {
	if t.syncCompleted.Load() {
		return nil
	}

	t.lock.RLock()
	defer t.lock.RUnlock()

	for _, trie := range t.triesInProgress {
		for _, segment := range trie.segments {
			if err := segment.batch.Write(); err != nil {
				log.Error("failed to write segment batch on finalize", "err", err)
				return err
			}
		}
	}
	return nil
}
