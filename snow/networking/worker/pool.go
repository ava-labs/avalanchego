// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"errors"
	"sync"
)

var (
	_ Pool = (*pool)(nil)

	ErrNoWorkers = errors.New("attempting to create worker pool with less than 1 worker")
)

type Request func()

type Pool interface {
	// Starts the worker pool.
	//
	// This method should be called before Send and Shutdown.
	Start()

	// Send the request to the worker pool.
	//
	// Send can be safely called before [Start] and it'll be no-op.
	// Send can be safely called after [Shutdown] and it'll be no-op.
	Send(Request)

	// Shutdown the worker pool.
	//
	// This method will block until all workers have finished their current
	// tasks.
	//
	// Shutdown can be safely called before [Start] and it'll be no-op.
	// Shutdown can be safely called multiple times.
	Shutdown()
}

type pool struct {
	workersCount int
	requests     chan Request

	startOnce    sync.Once
	shutdownOnce sync.Once

	// [shutdownWG] makes sure all workers have stopped before Shutdown returns
	shutdownWG sync.WaitGroup

	// closing [quit] tells the workers to stop working
	quit chan struct{}
}

func NewPool(workersCount int) (Pool, error) {
	if workersCount <= 0 {
		return nil, ErrNoWorkers
	}

	p := &pool{
		workersCount: workersCount,
	}

	// Note: we instantiate requests and quit channels
	// only when start is called.
	return p, nil
}

func (p *pool) Start() {
	p.startOnce.Do(func() {
		p.requests = make(chan Request)
		p.quit = make(chan struct{})

		p.shutdownWG.Add(p.workersCount)
		for w := 0; w < p.workersCount; w++ {
			go p.runWorker()
		}
	})
}

func (p *pool) runWorker() {
	defer p.shutdownWG.Done()

	for {
		select {
		case <-p.quit:
			p.requests = nil
			return // stop worker
		case request := <-p.requests:
			if request != nil {
				request()
			}
		}
	}
}

func (p *pool) Send(msg Request) {
	if p.requests == nil {
		// out of order Send, Start has not been called yet
		return
	}

	select {
	case p.requests <- msg:
	case <-p.quit:
	}
}

func (p *pool) Shutdown() {
	if p.quit == nil {
		// out of order Shutdown, Start has not been called yet
		return
	}

	p.shutdownOnce.Do(func() {
		close(p.quit)
		// We don't close requests channel to avoid panics
		// upon sending request over a closed channel.
	})
	p.shutdownWG.Wait()
}
