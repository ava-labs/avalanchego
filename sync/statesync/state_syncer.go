// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/coreth/core/state/snapshot"
	synccommon "github.com/ava-labs/coreth/sync"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/triedb"
	"golang.org/x/sync/errgroup"
)

const (
	segmentThreshold       = 500_000 // if we estimate trie to have greater than this number of leafs, split it
	numStorageTrieSegments = 4
	numMainTrieSegments    = 8
	defaultNumWorkers      = 8
)

var _ synccommon.Syncer = (*stateSync)(nil)

type Config struct {
	BatchSize uint
	// Maximum number of code hashes in the code syncer queue.
	MaxOutstandingCodeHashes int
	NumCodeFetchingWorkers   int
	// Number of leafs to request from a peer at a time.
	// NOTE: user facing option validated as the parameter [plugin/evm/config.Config.StateSyncRequestSize].
	RequestSize uint16
}

// NewDefaultConfig returns a Config with the default values for the state syncer.
// TODO: as a next feature we should probably introduce functional options for the config, e.g. func WithRequestSize(requestSize uint16) SyncerOption,
// because this function is not very flexible.
func NewDefaultConfig(requestSize uint16) Config {
	return Config{
		BatchSize:                ethdb.IdealBatchSize,
		MaxOutstandingCodeHashes: defaultMaxOutstandingCodeHashes,
		NumCodeFetchingWorkers:   defaultNumCodeFetchingWorkers,
		RequestSize:              requestSize,
	}
}

// WithUnsetDefaults applies defaults for unset fields. Zero values are treated as
// "unset" and replaced with sensible defaults.
func (c Config) WithUnsetDefaults() Config {
	out := c
	if out.BatchSize == 0 {
		out.BatchSize = ethdb.IdealBatchSize
	}
	if out.MaxOutstandingCodeHashes == 0 {
		out.MaxOutstandingCodeHashes = defaultMaxOutstandingCodeHashes
	}
	if out.NumCodeFetchingWorkers == 0 {
		out.NumCodeFetchingWorkers = defaultNumCodeFetchingWorkers
	}

	return out
}

// stateSync keeps the state of the entire state sync operation.
type stateSync struct {
	db        ethdb.Database            // database we are syncing
	root      common.Hash               // root of the EVM state we are syncing to
	trieDB    *triedb.Database          // trieDB on top of db we are syncing. used to restore any existing tries.
	snapshot  snapshot.SnapshotIterable // used to access the database we are syncing as a snapshot.
	batchSize uint                      // write batches when they reach this size
	client    syncclient.Client         // used to contact peers over the network

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
}

func NewSyncer(client syncclient.Client, db ethdb.Database, root common.Hash, config Config) (synccommon.Syncer, error) {
	cfg := config.WithUnsetDefaults()

	ss := &stateSync{
		batchSize:       cfg.BatchSize,
		db:              db,
		client:          client,
		root:            root,
		trieDB:          triedb.NewDatabase(db, nil),
		snapshot:        snapshot.NewDiskLayer(db),
		stats:           newTrieSyncStats(),
		triesInProgress: make(map[common.Hash]*trieToSync),

		// [triesInProgressSem] is used to keep the number of tries syncing
		// less than or equal to [defaultNumWorkers].
		triesInProgressSem: make(chan struct{}, defaultNumWorkers),

		// Each [trieToSync] will have a maximum of [numSegments] segments.
		// We set the capacity of [segments] such that [defaultNumWorkers]
		// storage tries can sync concurrently.
		segments:         make(chan syncclient.LeafSyncTask, defaultNumWorkers*numStorageTrieSegments),
		mainTrieDone:     make(chan struct{}),
		storageTriesDone: make(chan struct{}),
		done:             make(chan error, 1),
	}

	ss.syncer = syncclient.NewCallbackLeafSyncer(client, ss.segments, &syncclient.LeafSyncerConfig{
		RequestSize: cfg.RequestSize,
		NumWorkers:  defaultNumWorkers,
		OnFailure:   ss.onSyncFailure,
	})

	var err error
	ss.codeSyncer, err = newCodeSyncer(client, db, cfg)
	if err != nil {
		return nil, err
	}

	ss.trieQueue = NewTrieQueue(db)
	if err := ss.trieQueue.clearIfRootDoesNotMatch(ss.root); err != nil {
		return nil, err
	}

	// create a trieToSync for the main trie and mark it as in progress.
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

func (t *stateSync) Sync(ctx context.Context) error {
	// Start the code syncer and leaf syncer.
	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		if err := t.syncer.Sync(egCtx); err != nil {
			return err
		}
		return t.onSyncComplete()
	})
	eg.Go(func() error {
		return t.codeSyncer.Sync(egCtx)
	})
	eg.Go(func() error {
		return t.storageTrieProducer(egCtx)
	})

	// The errgroup wait will take care of returning the first error that occurs, or returning
	// nil if syncing finish without an error.
	return eg.Wait()
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
func (t *stateSync) onSyncFailure() {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for _, trie := range t.triesInProgress {
		for _, segment := range trie.segments {
			if err := segment.batch.Write(); err != nil {
				log.Error("failed to write segment batch on sync failure", "err", err)
				return
			}
		}
	}
}
