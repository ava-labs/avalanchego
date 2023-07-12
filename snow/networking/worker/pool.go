// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import "sync"

var _ Pool = (*pool)(nil)

type Request func()

type Pool interface {
	// Send the request to the worker pool.
	//
	// Send can be safely called after [Shutdown] but it won't carry out the request.
	Send(Request)

	// Shutdown the worker pool.
	//
	// This method will block until all workers have finished their current
	// tasks.
	//
	// It is safe to call shutdown multiple times.
	Shutdown()
}

type pool struct {
	requests chan Request

	shutdownOnce sync.Once
	shutdownWG   sync.WaitGroup

	// close to signal the workers to stop working
	quit chan struct{}
}

func NewPool(size int) Pool {
	p := &pool{
		requests: make(chan Request),
		quit:     make(chan struct{}),
	}
	p.shutdownWG.Add(size)
	for w := 0; w < size; w++ {
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
			request()
		}
	}
}

func (p *pool) Shutdown() {
	p.shutdownOnce.Do(func() {
		close(p.quit)
		close(p.requests)
	})
	p.shutdownWG.Wait()
}

func (p *pool) Send(msg Request) {
	select {
	case p.requests <- msg:
	case <-p.quit:
	}
}
