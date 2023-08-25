// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import "sync"

var _ Pool = (*pool)(nil)

type Request func()

type Pool interface {
	// Send the request to the worker pool.
	//
	// Send can be safely called after [Shutdown] and it'll be no-op.
	Send(Request)

	// Shutdown the worker pool.
	//
	// This method will block until all workers have finished their current
	// tasks.
	//
	// Shutdown can be safely called multiple times.
	Shutdown()
}

type pool struct {
	requests chan Request

	// [shutdownOnce] ensures Shutdown idempotency
	shutdownOnce sync.Once

	// [shutdownWG] makes sure all workers have stopped before Shutdown returns
	shutdownWG sync.WaitGroup

	// closing [quit] tells the workers to stop working
	quit chan struct{}
}

func NewPool(workersCount int) Pool {
	p := &pool{
		requests: make(chan Request),
		quit:     make(chan struct{}),
	}

	for w := 0; w < workersCount; w++ {
		p.shutdownWG.Add(1)
		go p.runWorker()
	}

	return p
}

func (p *pool) runWorker() {
	defer p.shutdownWG.Done()

	for {
		select {
		case <-p.quit:
			return // stop worker
		case request := <-p.requests:
			if request != nil {
				request()
			}
		}
	}
}

func (p *pool) Send(msg Request) {
	select {
	case <-p.quit:
	case p.requests <- msg:
	}
}

func (p *pool) Shutdown() {
	p.shutdownOnce.Do(func() {
		close(p.quit)
		// We don't close requests channel to avoid panics
		// upon sending request over a closed channel.
	})
	p.shutdownWG.Wait()
}
