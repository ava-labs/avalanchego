// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import "sync"

var _ Pool = (*pool)(nil)

type Request func()

type Pool interface {
	// Send the request to the worker pool.
	//
	// Send is a no-op if [Shutdown] has been called.
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

	shutdownOnce sync.Once
	shutdownWG   sync.WaitGroup
	quit         chan struct{}
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
			return
		case request := <-p.requests:
			request()
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
		// Note: we don't close [p.requests] to avoid panics
		// upon [p.Send] begin called with a closed channel.
	})
	p.shutdownWG.Wait()
}
