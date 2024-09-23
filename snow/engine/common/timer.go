// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"sync"
	"time"
)

// PreemptionSignal signals when to preempt the pendingTimeout of the timeout handler.
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
	newTimer         func(duration time.Duration) *time.Timer
	onTimeout        func()
	preemptionSignal <-chan struct{}
	pendingTimeout   chan struct{}
}

// NewTimeoutScheduler constructs a new timeout scheduler with the given function to be invoked upon a timeout,
// unless the preemptionSignal is closed and in which case it invokes the function immediately.
func NewTimeoutScheduler(onTimeout func(), preemptionSignal <-chan struct{}, newTimer func(duration time.Duration) *time.Timer) *timeoutScheduler {
	pendingTimout := make(chan struct{}, 1)
	pendingTimout <- struct{}{}
	return &timeoutScheduler{
		preemptionSignal: preemptionSignal,
		newTimer:         newTimer,
		onTimeout:        onTimeout,
		pendingTimeout:   pendingTimout,
	}
}

// RegisterTimeout fires the function the timeout scheduler is initialized with no later than the given timeout.
func (th *timeoutScheduler) RegisterTimeout(d time.Duration) {
	acquiredToken := th.acquirePendingTimeoutToken()
	preempted := th.preempted()

	if !preempted && !acquiredToken {
		return
	}

	go th.scheduleTimeout(d, acquiredToken)
}

func (th *timeoutScheduler) scheduleTimeout(d time.Duration, acquiredToken bool) {
	timer := th.newTimer(d)
	defer timer.Stop()

	defer th.onTimeout()

	select {
	case <-timer.C:
	case <-th.preemptionSignal:
	}

	if acquiredToken {
		th.relinquishPendingTimeoutToken()
	}
}

func (th *timeoutScheduler) preempted() bool {
	select {
	case <-th.preemptionSignal:
		return true
	default:
		return false
	}
}

func (th *timeoutScheduler) acquirePendingTimeoutToken() bool {
	select {
	case <-th.pendingTimeout:
		return true
	default:
		return false
	}
}

func (th *timeoutScheduler) relinquishPendingTimeoutToken() {
	th.pendingTimeout <- struct{}{}
}

// TimeoutRegistrar describes the standard interface for specifying a timeout
type TimeoutRegistrar interface {
	// RegisterTimeout specifies how much time to delay the next timeout message
	// by. If the subnet has been bootstrapped, the timeout will fire
	// immediately via calling Preempt().
	RegisterTimeout(time.Duration)
}
