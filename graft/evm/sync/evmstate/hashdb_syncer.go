// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/sync/leaf"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const defaultLeafWorkers = 8

var (
	_ types.Syncer    = (*HashDBSyncer)(nil)
	_ types.Finalizer = (*HashDBSyncer)(nil)

	errRootRequired      = errors.New("root must be non-zero")
	errCodeQueueRequired = errors.New("code queue is required")
)

// codeProducer is the producer side of the code sync. The engine owns the
// queue's teardown, so this syncer only adds hashes and declares when it is done.
type codeProducer interface {
	codeEnqueuer
	// CloseInput reports that no more hashes will be added. It must not block,
	// because the consumer drains under the same context as this syncer.
	CloseInput()
}

// HashDBSyncer reconstructs an EVM state trie on the hashdb stack: the account trie
// first, then every storage trie it discovers, each split into concurrently fetched
// segments. Contract code goes to a [code.Queue] drained alongside Sync.
type HashDBSyncer struct {
	log       logging.Logger
	fetcher   types.LeafFetcher
	db        ethdb.Database
	root      common.Hash
	codeQueue codeProducer
	trieQueue *trieQueue
	stats     *trieSyncStats
	threshold uint64 // leaf count above which a trie splits into segments

	// scheduler owns the task channel and tracks what needs flushing on failure.
	scheduler *trieScheduler

	mainTrieDone chan struct{}
	mainTrie     *stateTrie
	completed    atomic.Bool
}

// NewHashDBSyncer returns a syncer for the account trie at root. codeQueue must
// run concurrently with Sync.
//
// The caller must wipe the account and storage snapshots in db unless this run resumes
// the root already persisted there. Leaves left behind count as resume progress, so
// another root's leaves would fail the final root check on every attempt.
func NewHashDBSyncer(log logging.Logger, fetcher types.LeafFetcher, db ethdb.Database, root common.Hash, codeQueue codeProducer) (*HashDBSyncer, error) {
	if root == (common.Hash{}) {
		return nil, errRootRequired
	}
	if codeQueue == nil {
		return nil, errCodeQueueRequired
	}
	return &HashDBSyncer{
		log:       log,
		fetcher:   fetcher,
		db:        db,
		root:      root,
		codeQueue: codeQueue,
		trieQueue: newTrieQueue(db),
		threshold: segmentThreshold,
	}, nil
}

// Name returns a human-readable name for logging.
func (*HashDBSyncer) Name() string { return "EVM State Syncer" }

// ID returns the stable identifier for deduplication and metrics.
func (*HashDBSyncer) ID() string { return "state_evm_state_sync" }

// Sync reconstructs the account trie, then every discovered storage trie, verifying
// each root.
func (s *HashDBSyncer) Sync(ctx context.Context) error {
	// Wipe stale markers so resume never builds on another target's progress.
	if err := s.trieQueue.clearIfRootDoesNotMatch(s.root); err != nil {
		return err
	}

	s.scheduler = newTrieScheduler(defaultLeafWorkers, numStorageTrieSegments)
	s.mainTrieDone = make(chan struct{})
	s.stats = newTrieSyncStats(s.log)

	mainTrie, err := newStateTrie(s.db, s.root, common.Hash{}, newAccountLeafStore(s.db, s.codeQueue, s.trieQueue), stateTrieConfig{
		numSegments: numMainTrieSegments,
		threshold:   s.threshold,
		tasks:       s.scheduler.tasks,
		onDone:      s.onMainTrieDone,
		isMainTrie:  true,
		stats:       s.stats,
	})
	if err != nil {
		return err
	}
	s.mainTrie = mainTrie
	if err := s.scheduler.queueMain(ctx, s.root, s.mainTrie); err != nil {
		return err
	}

	fetcher := leaf.NewSyncer(s.fetcher, s.scheduler.tasks, leaf.WithNumWorkers(defaultLeafWorkers))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		if err := fetcher.Sync(egCtx); err != nil {
			return err
		}
		return s.onSyncComplete()
	})
	eg.Go(func() error { return s.storageTrieProducer(egCtx) })
	return eg.Wait()
}

// onSyncComplete persists the account trie's nodes only once every storage trie is
// done, so the state root never lands on disk ahead of the state it commits to.
func (s *HashDBSyncer) onSyncComplete() error {
	if err := s.mainTrie.commitNodes(); err != nil {
		return err
	}
	s.completed.Store(true)
	return nil
}

// Finalize flushes in-progress snapshot writes after a failed or cancelled sync, so
// the next run resumes instead of re-fetching. A no-op once the sync completed.
//
// Call it only after [HashDBSyncer.Sync] returns: it writes without synchronizing
// against the workers.
func (s *HashDBSyncer) Finalize() error {
	if s.completed.Load() || s.scheduler == nil {
		return nil
	}
	return s.scheduler.flush()
}

// onMainTrieDone runs when the account trie is verified. Only account leaves
// carry code hashes, so it closes the code syncer's input and opens the gate
// for storage tries.
func (s *HashDBSyncer) onMainTrieDone(context.Context) error {
	s.codeQueue.CloseInput()

	remaining, err := s.trieQueue.countTries()
	if err != nil {
		return err
	}
	s.stats.setTriesRemaining(remaining)

	close(s.mainTrieDone)
	return nil
}

// storageTrieProducer waits for the account trie, then feeds every storage trie it
// discovers to the scheduler.
func (s *HashDBSyncer) storageTrieProducer(ctx context.Context) error {
	select {
	case <-s.mainTrieDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	for next, err := range s.trieQueue.storageTries() {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		storageTrie, err := newStateTrie(s.db, next.root, next.accounts[0], newStorageLeafStore(s.db, next.accounts), stateTrieConfig{
			numSegments: numStorageTrieSegments,
			threshold:   s.threshold,
			tasks:       s.scheduler.tasks,
			onDone:      s.storageTrieDone(next.root),
			stats:       s.stats,
		})
		if err != nil {
			return err
		}
		if err := s.scheduler.queueStorage(ctx, next.root, storageTrie); err != nil {
			return err
		}
	}

	return s.scheduler.closeWhenIdle(ctx)
}

// storageTrieDone clears a finished trie's markers and hands its slot back.
func (s *HashDBSyncer) storageTrieDone(root common.Hash) func(context.Context) error {
	return func(context.Context) error {
		// Deferred so a failed trie still releases, or the scheduler never goes idle.
		defer s.scheduler.finishStorage(root)

		if err := s.trieQueue.StorageTrieDone(root); err != nil {
			return err
		}
		s.stats.trieDone(root)
		return nil
	}
}
