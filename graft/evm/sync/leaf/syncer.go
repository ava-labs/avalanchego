// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leaf

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm/options"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

const (
	defaultWorkers = 8

	// defaultRequestSize caps a single range, matching the handler's own cap.
	defaultRequestSize = 1024
)

var (
	ErrFailedToFetchLeafs = errors.New("failed to fetch leafs")
	ErrMoreWithoutKeys    = errors.New("more leaves reported but none returned")
)

// Task is one unit of leaf work the syncer drives: a contiguous key range of a
// trie, with callbacks per batch and on completion.
type Task interface {
	Root() common.Hash
	Account() common.Hash
	Start() []byte
	// End is the inclusive last key of the range, or nil for the whole trie.
	End() []byte
	OnLeaves(ctx context.Context, leaves types.Leaves) error
	OnFinish(ctx context.Context) error
}

type config struct {
	numWorkers  int
	requestSize uint16
}

type Option = options.Option[config]

// WithNumWorkers overrides the number of concurrent workers.
func WithNumWorkers(n int) Option {
	return options.Func[config](func(c *config) {
		if n > 0 {
			c.numWorkers = n
		}
	})
}

// WithRequestSize overrides how many leaves a single range asks for.
func WithRequestSize(n uint16) Option {
	return options.Func[config](func(c *config) {
		if n > 0 {
			c.requestSize = n
		}
	})
}

// Syncer pulls tasks off a channel and fetches each one's leaves with a pool of
// workers, handing every batch to the task, which is what reconstructs. Batches
// are verified in the fetch path, not in the transport.
type Syncer struct {
	fetcher types.LeafFetcher
	tasks   <-chan Task
	config  config
}

func NewSyncer(fetcher types.LeafFetcher, tasks <-chan Task, opts ...Option) *Syncer {
	cfg := options.ApplyTo(&config{
		numWorkers:  defaultWorkers,
		requestSize: defaultRequestSize,
	}, opts...)
	return &Syncer{fetcher: fetcher, tasks: tasks, config: *cfg}
}

// Sync runs the workers until tasks is drained and closed, or ctx ends.
func (s *Syncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	for range s.config.numWorkers {
		eg.Go(func() error { return s.workerLoop(egCtx) })
	}
	return eg.Wait()
}

// workerLoop processes tasks until the channel closes or ctx ends.
func (s *Syncer) workerLoop(ctx context.Context) error {
	for {
		select {
		case t, ok := <-s.tasks:
			if !ok {
				return nil
			}
			if err := s.syncTask(ctx, t); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// syncTask walks the task's range left to right until it is exhausted or End is reached.
func (s *Syncer) syncTask(ctx context.Context, t Task) error {
	start := t.Start()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		leaves, err := s.fetcher.FetchLeaves(ctx, types.LeafRange{
			Root:    t.Root(),
			Account: t.Account(),
			Start:   start,
			Limit:   s.config.requestSize,
		})
		if err != nil {
			return fmt.Errorf("%w from %x: %w", ErrFailedToFetchLeafs, start, err)
		}

		// End is bounded here, not on the wire, because VerifyRangeProof mishandles
		// an empty response with a non-empty end.
		exhausted := truncate(&leaves, t.End())

		if err := t.OnLeaves(ctx, leaves); err != nil {
			return err
		}

		if exhausted || !leaves.More {
			return t.OnFinish(ctx)
		}
		if len(leaves.Keys) == 0 {
			// more with no keys would loop forever.
			return ErrMoreWithoutKeys
		}
		start = NextRangeKey(lastKey(leaves))
	}
}

// lastKey returns the highest key, the next request's start. Not valid when empty.
func lastKey(leaves types.Leaves) []byte {
	return leaves.Keys[len(leaves.Keys)-1]
}

// truncate drops leaves past end and reports whether it cut any, meaning the
// range is exhausted. An empty end is a no-op.
func truncate(leaves *types.Leaves, end []byte) bool {
	if len(end) == 0 {
		return false
	}
	// Keys ascend, so the first one past end bounds the run.
	n := sort.Search(len(leaves.Keys), func(i int) bool { return !WithinRange(leaves.Keys[i], end) })
	if n == len(leaves.Keys) {
		return false
	}
	leaves.Keys, leaves.Vals = leaves.Keys[:n], leaves.Vals[:n]
	return true
}
