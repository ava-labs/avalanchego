// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/graft/evm/sync/leaf"
	"github.com/ava-labs/libevm/common"
)

// trieScheduler admits tries to the shared leaf fetcher, feeding each one's segments
// onto the leaf.Task channel, tracking what needs flushing, and closing that channel once.
//
// The bound and the close are one mechanism: a trie holds a slot while it runs, so
// holding every slot proves nothing is running and the channel is safe to close.
type trieScheduler struct {
	tasks chan leaf.Task
	slots chan struct{}

	lock  sync.Mutex
	tries map[common.Hash]*stateTrie
}

// newTrieScheduler admits workers tries at a time, buffering a full segment set each.
func newTrieScheduler(workers, segmentsPerTrie int) *trieScheduler {
	s := &trieScheduler{
		tasks: make(chan leaf.Task, workers*segmentsPerTrie),
		slots: make(chan struct{}, workers),
		tries: make(map[common.Hash]*stateTrie),
	}
	for range cap(s.slots) {
		s.slots <- struct{}{}
	}
	return s
}

// queueMain admits the account trie without a slot: it runs before any storage trie.
func (s *trieScheduler) queueMain(ctx context.Context, root common.Hash, trie *stateTrie) error {
	s.track(root, trie)
	return s.queue(ctx, trie)
}

// queueStorage waits for a free slot, held until [trieScheduler.finishStorage].
func (s *trieScheduler) queueStorage(ctx context.Context, root common.Hash, trie *stateTrie) error {
	select {
	case <-s.slots:
	case <-ctx.Done():
		return ctx.Err()
	}

	s.track(root, trie)
	return s.queue(ctx, trie)
}

// finishStorage releases a completed storage trie's slot and stops tracking it.
func (s *trieScheduler) finishStorage(root common.Hash) {
	s.lock.Lock()
	delete(s.tries, root)
	s.lock.Unlock()

	s.slots <- struct{}{}
}

// closeWhenIdle waits for every started trie to finish, then closes the leaf.Task channel
// so the workers exit. Reclaiming all slots proves no worker can still split a trie
// into fresh segments. A cancelled ctx leaves it open, and the workers exit via ctx.
func (s *trieScheduler) closeWhenIdle(ctx context.Context) error {
	for range cap(s.slots) {
		select {
		case <-s.slots:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	close(s.tasks)
	return nil
}

// flush writes the buffered progress of every tracked trie, so the next run resumes
// from it. Only safe with no worker running: a segment's batch is not concurrent-safe.
func (s *trieScheduler) flush() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, trie := range s.tries {
		if err := trie.flushProgress(); err != nil {
			return err
		}
	}
	return nil
}

func (s *trieScheduler) track(root common.Hash, trie *stateTrie) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.tries[root] = trie
}

func (s *trieScheduler) queue(ctx context.Context, trie *stateTrie) error {
	for _, segment := range trie.segments {
		select {
		case s.tasks <- segment:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
