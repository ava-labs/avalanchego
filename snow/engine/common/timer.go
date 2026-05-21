// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"sync"
	"time"
)

// PreemptionSignal signals when to preempt the pendingTimeoutToken of the timeout handler.
type PreemptionSignal struct {
	activateOnce sync.Once
	initOnce     sync.Once
	signal       chan struct{}
}

func (ps *PreemptionSignal) init() {
	ps.signal = make(chan struct{})
}

// Listen returns a read-only channel that is closed when Preempt() is invoked.
func (ps *PreemptionSignal) Listen() <-chan struct{} {
	ps.initOnce.Do(ps.init)
	return ps.signal
}

// Preempt causes any past and future calls of Listen to return a closed channel.
func (ps *PreemptionSignal) Preempt() {
	ps.initOnce.Do(ps.init)
	ps.activateOnce.Do(func() {
		close(ps.signal)
	})
}

// timeoutScheduler schedules timeouts to be dispatched in the future.
// Only a single timeout can be pending to be scheduled at any given time.
// Once a preemption signal is closed, all timeouts are immediately dispatched.
type timeoutScheduler struct {
	newTimer            func(duration time.Duration) *time.Timer
	onTimeout           func()
	preemptionSignal    <-chan struct{}
	pendingTimeoutToken chan struct{}
}

// NewTimeoutScheduler constructs a new timeout scheduler with the given function to be invoked upon a timeout,
// unless the preemptionSignal is closed and in which case it invokes the function immediately.
func NewTimeoutScheduler(onTimeout func(), preemptionSignal <-chan struct{}) *timeoutScheduler {
	pendingTimout := make(chan struct{}, 1)
	pendingTimout <- struct{}{}
	return &timeoutScheduler{
		preemptionSignal:    preemptionSignal,
		newTimer:            time.NewTimer,
		onTimeout:           onTimeout,
		pendingTimeoutToken: pendingTimout,
	}
}

// RegisterTimeout fires the function the timeout scheduler is initialized with no later than the given timeout.
func (th *timeoutScheduler) RegisterTimeout(d time.Duration) {
	// There can only be a single timeout pending at any time, and once a timeout is scheduled,
	// we prevent future timeouts to be scheduled until the timeout triggers by taking the pendingTimeoutToken.
	// Any subsequent attempt to register a timeout would fail obtaining the pendingTimeoutToken,
	// and return.
	if !th.acquirePendingTimeoutToken() {
		return
	}

	go th.scheduleTimeout(d)
}

func (th *timeoutScheduler) scheduleTimeout(d time.Duration) {
	timer := th.newTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-th.preemptionSignal:
	}

	// Relinquish the pendingTimeoutToken.
	// This is needed to be done before onTimeout() is invoked,
	// and that's why onTimeout() is deferred to be called at the end of the function.
	// If we trigger the timeout prematurely before we relinquish the pendingTimeoutToken,
	// A subsequent timeout scheduling attempt that originates from the triggering of the current timeout
	// will fail, as the pendingTimeoutToken is not yet available.
	th.pendingTimeoutToken <- struct{}{}

	th.onTimeout()
}

func (th *timeoutScheduler) acquirePendingTimeoutToken() bool {
	select {
	case <-th.pendingTimeoutToken:
		return true
	default:
		return false
	}
}

// TimeoutRegistrar describes the standard interface for specifying a timeout
type TimeoutRegistrar interface {
	// RegisterTimeout specifies how much time to delay the next timeout message by.
	//
	// If there is already a pending timeout message, this call is a no-op.
	// However, it is guaranteed that the timeout will fire at least once after
	// calling this function.
	//
	// If the subnet has been bootstrapped, the timeout will fire immediately via calling Preempt().
	RegisterTimeout(time.Duration)
}
