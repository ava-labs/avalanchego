// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/triedb"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/leaf"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

const (
	segmentThreshold       = 500_000 // if we estimate trie to have greater than this number of leafs, split it
	numStorageTrieSegments = 4
	numMainTrieSegments    = 8
	defaultNumWorkers      = 8

	// StateSyncerID is the stable identifier for the EVM state syncer.
	StateSyncerID = "state_evm_state_sync"
)

var (
	_                           types.Syncer = (*HashDBSyncer)(nil)
	errCodeRequestQueueRequired              = errors.New("code request queue is required")
	errLeafsRequestSizeRequired              = errors.New("leafs request size must be > 0")
)

// HashDBSyncer keeps the state of a single-root state sync session.
type HashDBSyncer struct {
	db        ethdb.Database            // database we are syncing
	root      common.Hash               // root of the EVM state we are syncing to
	trieDB    *triedb.Database          // trieDB on top of db we are syncing. used to restore any existing tries.
	snapshot  snapshot.SnapshotIterable // used to access the database we are syncing as a snapshot.
	batchSize uint                      // write batches when they reach this size
	segments  chan leaf.SyncTask        // channel of tasks to sync
	syncer    *leaf.CallbackSyncer      // performs the sync, looping over each task's range and invoking specified callbacks
	codeQueue types.CodeRequestQueue    // queue that manages the asynchronous download and batching of code hashes
	trieQueue *trieQueue                // manages a persistent list of storage tries we need to sync and any segments that are created for them

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

	// finalizeCodeQueue is called when the main trie completes.
	// No-op by default - set via WithFinalizeCodeQueue.
	finalizeCodeQueue func() error

	preserveSegments  bool              // keep segment markers across root changes
	storageTrieFilter StorageTrieFilter // skip unchanged storage tries on pivot
}

// HashDBSyncerOption configures the state syncer via functional options.
type HashDBSyncerOption = options.Option[HashDBSyncer]

// WithBatchSize sets the database batch size for writes.
func WithBatchSize(n uint) HashDBSyncerOption {
	return options.Func[HashDBSyncer](func(s *HashDBSyncer) {
		if n > 0 {
			s.batchSize = n
		}
	})
}

// WithFinalizeCodeQueue sets the callback invoked when the main trie completes.
// Static callers use this to finalize the code queue. If not set, it defaults
// to a no-op (used by the dynamic wrapper which manages code queue lifecycle).
func WithFinalizeCodeQueue(fn func() error) HashDBSyncerOption {
	return options.Func[HashDBSyncer](func(s *HashDBSyncer) {
		s.finalizeCodeQueue = fn
	})
}

// WithPreserveSegments keeps segment markers across root changes so
// unchanged storage tries resume from their prior sync position.
func WithPreserveSegments() HashDBSyncerOption {
	return options.Func[HashDBSyncer](func(s *HashDBSyncer) {
		s.preserveSegments = true
	})
}

// StorageTrieFilter returns true to skip syncing a storage trie.
type StorageTrieFilter func(db ethdb.Database, accountHash common.Hash, storageRoot common.Hash) bool

// WithStorageTrieFilter sets a filter to skip unchanged storage tries.
func WithStorageTrieFilter(fn StorageTrieFilter) HashDBSyncerOption {
	return options.Func[HashDBSyncer](func(s *HashDBSyncer) {
		s.storageTrieFilter = fn
	})
}

// NewHashDBSyncer creates a single-session state syncer for the given root.
func NewHashDBSyncer(syncClient client.Client, db ethdb.Database, root common.Hash, codeQueue types.CodeRequestQueue, leafsRequestSize uint16, leafsRequestType message.LeafsRequestType, opts ...HashDBSyncerOption) (*HashDBSyncer, error) {
	if leafsRequestSize == 0 {
		return nil, errLeafsRequestSizeRequired
	}
	if codeQueue == nil {
		return nil, errCodeRequestQueueRequired
	}
	ss := &HashDBSyncer{
		db:                db,
		root:              root,
		trieDB:            triedb.NewDatabase(db, nil),
		snapshot:          snapshot.NewDiskLayer(db),
		codeQueue:         codeQueue,
		batchSize:         ethdb.IdealBatchSize,
		finalizeCodeQueue: func() error { return nil },
		stats:             newTrieSyncStats(),
		triesInProgress:   make(map[common.Hash]*trieToSync),

		// [triesInProgressSem] is used to keep the number of tries syncing
		// less than or equal to [defaultNumWorkers].
		triesInProgressSem: make(chan struct{}, defaultNumWorkers),

		// Each [trieToSync] will have a maximum of [numStorageTrieSegments] segments.
		// We set the capacity of [segments] such that [defaultNumWorkers]
		// storage tries can sync concurrently.
		segments:         make(chan leaf.SyncTask, defaultNumWorkers*numStorageTrieSegments),
		mainTrieDone:     make(chan struct{}),
		storageTriesDone: make(chan struct{}),
	}

	// Apply functional options.
	options.ApplyTo(ss, opts...)

	ss.syncer = leaf.NewCallbackSyncer(syncClient, ss.segments, &leaf.SyncerConfig{
		RequestSize:      leafsRequestSize,
		NumWorkers:       defaultNumWorkers,
		LeafsRequestType: leafsRequestType,
	})

	ss.trieQueue = NewTrieQueue(db)
	if err := ss.trieQueue.clearIfRootDoesNotMatch(root, ss.preserveSegments); err != nil {
		return nil, err
	}

	// create a trieToSync for the main trie and mark it as in progress.
	var err error
	ss.mainTrie, err = NewTrieToSync(ss, root, common.Hash{}, NewMainTrieTask(ss))
	if err != nil {
		return nil, err
	}
	ss.addTrieInProgress(root, ss.mainTrie)

	// Use context.Background() for initialization since we don't have a sync context yet.
	// This is safe because startSyncing only enqueues segments.
	if err := ss.mainTrie.startSyncing(context.Background()); err != nil {
		return nil, err
	}
	return ss, nil
}

// Name returns the human-readable name for this sync task.
func (*HashDBSyncer) Name() string {
	return "HashDB EVM State Syncer (static)"
}

// ID returns the stable identifier for this sync task.
func (*HashDBSyncer) ID() string {
	return StateSyncerID
}

// Sync runs the single-session sync to completion.
func (t *HashDBSyncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		if err := t.syncer.Sync(egCtx); err != nil {
			return err
		}
		return t.onSyncComplete()
	})

	eg.Go(func() error {
		return t.storageTrieProducer(egCtx)
	})

	return eg.Wait()
}

// UpdateTarget is a no-op for the static syncer.
func (*HashDBSyncer) UpdateTarget(message.Syncable) error { return nil }

// onStorageTrieFinished is called after a storage trie finishes syncing.
func (t *HashDBSyncer) onStorageTrieFinished(root common.Hash) error {
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
func (t *HashDBSyncer) onMainTrieFinished() error {
	if err := t.finalizeCodeQueue(); err != nil {
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
func (t *HashDBSyncer) onSyncComplete() error {
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
func (t *HashDBSyncer) storageTrieProducer(ctx context.Context) error {
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
func (t *HashDBSyncer) addTrieInProgress(root common.Hash, trie *trieToSync) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.triesInProgress[root] = trie
}

// removeTrieInProgress removes root from the set of tracked tries in progress
// and returns the number of tries in progress after the removal.
func (t *HashDBSyncer) removeTrieInProgress(root common.Hash) (int, error) {
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
func (t *HashDBSyncer) Finalize() error {
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
