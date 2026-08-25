// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
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
)

// CodeProducer is the producer side of the code sync. The engine owns the
// queue's teardown, so this syncer only adds hashes and says when it is done.
type CodeProducer interface {
	AddCode(hashes []common.Hash) error
	// DoneAdding reports that no more hashes will be added. It must not block,
	// because the consumer drains under the same context as this syncer.
	DoneAdding()
}

// HashDBSyncer reconstructs EVM state on the hashdb stack: the account trie, then
// every storage trie it finds, each split into concurrently fetched segments.
type HashDBSyncer struct {
	fetcher   types.LeafFetcher
	db        ethdb.Database
	root      common.Hash
	codeQueue CodeProducer // the engine owns its teardown, this syncer only feeds it
	trieQueue *trieQueue   // durable storage-trie markers, so a restart resumes
	threshold uint64       // leaf count above which a trie splits into segments

	scheduler    *trieScheduler // tracks what needs flushing on failure
	stats        *trieSyncStats
	mainTrieDone chan struct{} // closed once the account trie is verified
	mainTrie     *stateTrie    // nodes commit only after every storage trie is done
	completed    atomic.Bool   // makes Finalize a no-op after a clean run
}

// NewHashDBSyncer syncs the account trie at root. codeQueue must drain concurrently
// with Sync, and the caller must wipe the snapshots unless resuming this same root.
func NewHashDBSyncer(log logging.Logger, fetcher types.LeafFetcher, db ethdb.Database, root common.Hash, codeQueue CodeProducer) *HashDBSyncer {
	return &HashDBSyncer{
		fetcher:   fetcher,
		db:        db,
		root:      root,
		codeQueue: codeQueue,
		trieQueue: newTrieQueue(db),
		threshold: segmentThreshold,

		scheduler:    newTrieScheduler(defaultLeafWorkers, numStorageTrieSegments),
		mainTrieDone: make(chan struct{}),
		stats:        newTrieSyncStats(log),
	}
}

func (*HashDBSyncer) Name() string { return "EVM State Syncer" }

func (*HashDBSyncer) ID() string { return "state_evm_state_sync" }

func (s *HashDBSyncer) Sync(ctx context.Context) error {
	// Wipe stale markers so resume never builds on another target's progress.
	if err := s.trieQueue.clearIfRootDoesNotMatch(s.root); err != nil {
		return err
	}

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

// Finalize flushes in-progress writes so the next run resumes instead of re-fetching.
// A no-op once synced, and lock-free, so call it only after Sync returns.
func (s *HashDBSyncer) Finalize() error {
	if s.completed.Load() {
		return nil
	}
	return s.scheduler.flush()
}

// onMainTrieDone runs when the account trie is verified. Only account leaves carry
// code hashes, so this is where the code input closes and storage tries start.
func (s *HashDBSyncer) onMainTrieDone(context.Context) error {
	s.codeQueue.DoneAdding()

	remaining, err := s.trieQueue.countTries()
	if err != nil {
		return err
	}
	s.stats.setTriesRemaining(remaining)

	close(s.mainTrieDone)
	return nil
}

// storageTrieProducer feeds the scheduler every storage trie the account trie found.
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
