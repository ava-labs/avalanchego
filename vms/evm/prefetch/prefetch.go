// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefetch

import "github.com/ava-labs/libevm/core/state"

type workerPool struct {
	*boundedWorkers
}

func (wp *workerPool) Done() {
	// Done is only called after all work is complete, so Wait simply lets the
	// remaining goroutines finish.
	wp.boundedWorkers.Wait()
}

// WithConcurrentWorkers sets the maximum number of goroutines that trie
// prefetching uses to load nodes.
func WithConcurrentWorkers(prefetchers int) state.PrefetcherOption {
	pool := &workerPool{
		boundedWorkers: newBoundedWorkers(prefetchers),
	}
	return state.WithWorkerPools(func() state.WorkerPool { return pool })
}
