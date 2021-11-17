// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"time"
)

// Timer wraps a timer object. This allows a user to specify a handler. Once
// specifying the handler, the dispatch thread can be called. The dispatcher
// will only return after calling Stop. SetTimeoutIn will result in calling the
// handler in the specified amount of time.
type Timer struct {
	handler func()
	timeout chan struct{}

	lock                    sync.Mutex
	wg                      sync.WaitGroup
	finished, shouldExecute bool
	duration                time.Duration
}

// NewTimer creates a new timer object
func NewTimer(handler func()) *Timer {
	timer := &Timer{
		handler: handler,
		timeout: make(chan struct{}, 1),
	}
	timer.wg.Add(1)

	return timer
}

// SetTimeoutIn will set the timer to fire the handler in [duration]
func (t *Timer) SetTimeoutIn(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.duration = duration
	t.shouldExecute = true
	t.reset()
}

// Cancel the currently scheduled event
func (t *Timer) Cancel() {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.shouldExecute = false
	t.reset()
}

// Stop this timer from executing any more.
func (t *Timer) Stop() {
	t.lock.Lock()
	if !t.finished {
		defer t.wg.Wait()
	}
	defer t.lock.Unlock()

	t.finished = true
	t.reset()
}

func (t *Timer) Dispatch() {
	t.lock.Lock()
	defer t.lock.Unlock()
	defer t.wg.Done()

	timer := time.NewTimer(0)
	cleared := false
	reset := false
	for !t.finished { // t.finished needs to be thread safe
		if !reset && !timer.Stop() && !cleared {
			<-timer.C
		}

		if cleared && t.shouldExecute {
			t.lock.Unlock()
			t.handler()
		} else {
			t.lock.Unlock()
		}

		cleared = false
		reset = false
		select {
		case <-t.timeout:
			t.lock.Lock()
			if t.shouldExecute {
				timer.Reset(t.duration)
			}
			reset = true
		case <-timer.C:
			t.lock.Lock()
			cleared = true
		}
	}
}

func (t *Timer) reset() {
	select {
	case t.timeout <- struct{}{}:
	default:
	}
}
