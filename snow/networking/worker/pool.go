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
	workersCount int
	requests     chan Request

	shutdown     bool
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
		requests:     make(chan Request),
		quit:         make(chan struct{}),
	}

	p.shutdownWG.Add(p.workersCount)
	for w := 0; w < p.workersCount; w++ {
		go p.runWorker()
	}

	return p, nil
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
	if p.shutdown {
		return
	}

	select {
	case p.requests <- msg:
	case <-p.quit:
		p.shutdown = true
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
