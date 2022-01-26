// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"sync"
)

var _ Pool = &pool{}

type Request func()

type Pool interface {
	// Send the request to the worker pool.
	//
	// Send should never be called after [Shutdown] is called.
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
}

func NewPool(size int) Pool {
	p := &pool{
		requests: make(chan Request),
	}
	p.shutdownWG.Add(size)
	for w := 0; w < size; w++ {
		go p.runWorker()
	}
	return p
}

func (p *pool) runWorker() {
	defer p.shutdownWG.Done()

	for request := range p.requests {
		request()
	}
}

func (p *pool) Shutdown() {
	p.shutdownOnce.Do(func() {
		close(p.requests)
	})
	p.shutdownWG.Wait()
}

func (p *pool) Send(msg Request) {
	p.requests <- msg
}
