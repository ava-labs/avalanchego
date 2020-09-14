// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
)

// Executor ...
type Executor struct {
	lock     sync.Mutex
	cond     *sync.Cond
	wg       sync.WaitGroup
	finished bool
	events   []func()
}

// Initialize ...
func (e *Executor) Initialize() {
	e.cond = sync.NewCond(&e.lock)
	e.wg.Add(1)
}

// Add new function to call
func (e *Executor) Add(event func()) {
	e.lock.Lock()
	defer e.lock.Unlock()

	e.events = append(e.events, event)
	e.cond.Signal()
}

// Stop executing functions
func (e *Executor) Stop() {
	e.lock.Lock()
	if !e.finished {
		defer e.wg.Wait()
	}
	defer e.lock.Unlock()

	e.finished = true
	e.cond.Broadcast()
}

// Dispatch the events. Will only return after stop is called.
func (e *Executor) Dispatch() {
	e.lock.Lock()
	defer e.lock.Unlock()
	defer e.wg.Done()

	for !e.finished {
		if len(e.events) == 0 {
			e.cond.Wait()
		} else {
			event := e.events[0]
			e.events = e.events[1:]

			e.lock.Unlock()
			event()
			e.lock.Lock()
		}
	}
}
