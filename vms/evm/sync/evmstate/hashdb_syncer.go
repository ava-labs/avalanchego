// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/types"
)

var (
	_ types.Syncer    = (*HashDBSyncer)(nil)
	_ types.Finalizer = (*HashDBSyncer)(nil)

	errCodeQueueRequired = errors.New("code queue is required")
)

// HashDBSyncer reconstructs an EVM state trie on the hashdb stack. A single
// worker pool drives the account trie and then every discovered storage trie,
// each split into segments that fetch concurrently. Contract code is fetched by
// a separate code [code.Syncer] draining the shared [code.Queue], which must run
// concurrently with Sync.
type HashDBSyncer struct {
	log       logging.Logger
	client    *Client
	db        ethdb.Database
	root      common.Hash
	codeQueue *code.Queue
	trieQueue *trieQueue
	stats     *trieSyncStats
	threshold uint64 // leaf count above which a trie splits into segments

	segments           chan task
	mainTrieDone       chan struct{}
	storageTriesDone   chan struct{}
	triesInProgressSem chan struct{}

	mainTrie  *stateTrie
	completed atomic.Bool

	lock            sync.Mutex
	triesInProgress map[common.Hash]*stateTrie
}

// NewHashDBSyncer returns a [HashDBSyncer] for the account trie at root. codeQueue
// receives the code hashes discovered while syncing and must be drained by a
// concurrently running code [code.Syncer]. log reports sync progress.
func NewHashDBSyncer(log logging.Logger, client *Client, db ethdb.Database, root common.Hash, codeQueue *code.Queue) (*HashDBSyncer, error) {
	if root == (common.Hash{}) {
		return nil, errRootRequired
	}
	if codeQueue == nil {
		return nil, errCodeQueueRequired
	}
	return &HashDBSyncer{
		log:       log,
		client:    client,
		db:        db,
		root:      root,
		codeQueue: codeQueue,
		trieQueue: newTrieQueue(db),
		threshold: segmentThreshold,
	}, nil
}

// Name returns a human-readable name for logging.
func (*HashDBSyncer) Name() string { return "EVM State Syncer" }

// ID returns the stable identifier used for deduplication and metrics.
func (*HashDBSyncer) ID() string { return "state_evm_state_sync" }

// Sync reconstructs the account trie, then every discovered storage trie,
// verifying each root. It records the target root on entry for a later resume
// check.
func (s *HashDBSyncer) Sync(ctx context.Context) error {
	// Always tear down the code queue forwarder on exit so it cannot leak or
	// block on a consumer that has stopped.
	defer s.codeQueue.Shutdown()

	// Records the target root, wiping stale markers if the previous sync targeted
	// a different root, so resume never builds on another target's progress.
	if err := s.trieQueue.clearIfRootDoesNotMatch(s.root); err != nil {
		return err
	}

	s.segments = make(chan task, defaultLeafWorkers*numStorageTrieSegments)
	s.mainTrieDone = make(chan struct{})
	s.storageTriesDone = make(chan struct{})
	s.triesInProgressSem = make(chan struct{}, defaultLeafWorkers)
	s.triesInProgress = make(map[common.Hash]*stateTrie)
	s.stats = newTrieSyncStats(s.log)

	mainTrie, err := newStateTrie(s.db, s.root, common.Hash{}, newAccountLeaves(s.db, s.codeQueue, s.trieQueue), stateTrieConfig{
		numSegments: numMainTrieSegments,
		threshold:   s.threshold,
		tasks:       s.segments,
		onDone:      s.onMainTrieDone,
		isMainTrie:  true,
		stats:       s.stats,
	})
	if err != nil {
		return err
	}
	s.mainTrie = mainTrie
	if err := s.queueSegments(ctx, s.mainTrie); err != nil {
		return err
	}

	pool := newCallbackSyncer(s.client, s.segments, defaultLeafWorkers)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		if err := pool.sync(egCtx); err != nil {
			return err
		}
		return s.onSyncComplete()
	})
	eg.Go(func() error { return s.storageTrieProducer(egCtx) })
	return eg.Wait()
}

// onSyncComplete persists the account trie's node batch after every storage trie
// has finished, so the state root never lands on disk ahead of the state it
// commits to.
func (s *HashDBSyncer) onSyncComplete() error {
	if err := s.mainTrie.batch.Write(); err != nil {
		return err
	}
	s.completed.Store(true)
	return nil
}

// Finalize flushes the snapshot writes of every in-progress trie on a failed or
// cancelled sync, so the next run resumes from the persisted progress rather
// than re-fetching it. It is a no-op once the sync has completed.
func (s *HashDBSyncer) Finalize() error {
	if s.completed.Load() {
		return nil
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	tries := make([]*stateTrie, 0, len(s.triesInProgress)+1)
	if s.mainTrie != nil {
		tries = append(tries, s.mainTrie)
	}
	for _, trie := range s.triesInProgress {
		tries = append(tries, trie)
	}

	for _, trie := range tries {
		for _, segment := range trie.segments {
			if err := segment.batch.Write(); err != nil {
				return err
			}
		}
	}
	return nil
}

// onMainTrieDone runs when the account trie is verified. No more code is
// discovered afterwards, so it drains the code queue and opens the gate for
// storage tries.
func (s *HashDBSyncer) onMainTrieDone(ctx context.Context) error {
	if err := s.finalizeCodeQueue(ctx); err != nil {
		return err
	}

	remaining, err := s.trieQueue.countTries()
	if err != nil {
		return err
	}
	s.stats.setTriesRemaining(remaining)

	close(s.mainTrieDone)
	return nil
}

// storageTrieProducer waits for the account trie to finish, then feeds storage
// tries into the shared pool, capping concurrent tries with the semaphore. It
// closes the segments channel once the last storage trie has been queued and no
// tries remain in progress.
func (s *HashDBSyncer) storageTrieProducer(ctx context.Context) error {
	select {
	case <-s.mainTrieDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		root, accounts, more, err := s.trieQueue.getNextTrie()
		if err != nil {
			return err
		}
		if root == (common.Hash{}) && !more {
			// No storage tries at all.
			close(s.segments)
			return nil
		}

		select {
		case s.triesInProgressSem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		storageTrie, err := newStateTrie(s.db, root, accounts[0], newStorageLeaves(s.db, accounts), stateTrieConfig{
			numSegments: numStorageTrieSegments,
			threshold:   s.threshold,
			tasks:       s.segments,
			onDone:      s.storageTrieDone(root),
			stats:       s.stats,
		})
		if err != nil {
			return err
		}
		s.addTrieInProgress(root, storageTrie)
		if !more {
			close(s.storageTriesDone)
		}
		if err := s.queueSegments(ctx, storageTrie); err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

// storageTrieDone returns the completion callback for a storage trie: it
// releases the semaphore, clears the trie's markers, and closes the segments
// channel once it is the last trie to finish.
func (s *HashDBSyncer) storageTrieDone(root common.Hash) func(context.Context) error {
	return func(context.Context) error {
		<-s.triesInProgressSem
		if err := s.trieQueue.StorageTrieDone(root); err != nil {
			return err
		}
		s.stats.trieDone(root)

		s.lock.Lock()
		delete(s.triesInProgress, root)
		remaining := len(s.triesInProgress)
		s.lock.Unlock()

		if remaining == 0 {
			// Close only after the producer has queued the last trie.
			select {
			case <-s.storageTriesDone:
				close(s.segments)
			default:
			}
		}
		return nil
	}
}

// queueSegments feeds a trie's segments into the shared pool.
func (s *HashDBSyncer) queueSegments(ctx context.Context, trie *stateTrie) error {
	for _, seg := range trie.segments {
		select {
		case s.segments <- seg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *HashDBSyncer) addTrieInProgress(root common.Hash, trie *stateTrie) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.triesInProgress[root] = trie
}

// finalizeCodeQueue drains the code queue to completion, abandoning the drain if
// ctx is cancelled so a stopped consumer cannot block the sync forever. Unsent
// hashes stay on disk as markers and recover on the next run.
func (s *HashDBSyncer) finalizeCodeQueue(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.codeQueue.Finalize()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.codeQueue.Shutdown()
		<-done
		return ctx.Err()
	}
}
