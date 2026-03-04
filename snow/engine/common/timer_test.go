// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"sync"
	"testing"
	"time"
)

func TestTimeoutScheduler(t *testing.T) {
	for _, testCase := range []struct {
		expectedInvocationCount int
		desc                    string
		shouldPreempt           bool
		clock                   chan time.Time
		initClock               func(chan time.Time)
		advanceTime             func(chan time.Time)
	}{
		{
			desc:                    "multiple pendingTimeoutToken one after the other with preemption",
			expectedInvocationCount: 10,
			shouldPreempt:           true,
			clock:                   make(chan time.Time, 1),
			initClock:               func(chan time.Time) {},
			advanceTime:             func(chan time.Time) {},
		},
		{
			desc:                    "multiple pendingTimeoutToken one after the other",
			expectedInvocationCount: 10,
			clock:                   make(chan time.Time, 1),
			initClock: func(clock chan time.Time) {
				clock <- time.Now()
			},
			advanceTime: func(clock chan time.Time) {
				clock <- time.Now()
			},
		},
	} {
		t.Run(testCase.desc, func(*testing.T) {
			// Not enough invocations means the test would stall.
			// Too many invocations means a negative counter panic.
			var wg sync.WaitGroup
			wg.Add(testCase.expectedInvocationCount)

			testCase.initClock(testCase.clock)

			var preemptionSignal PreemptionSignal
			ps := preemptionSignal.Listen()

			if testCase.shouldPreempt {
				preemptionSignal.Preempt()
			}

			// Order enforces timeouts to be registered once after another,
			// in order to make the tests deterministic.
			order := make(chan struct{})

			newTimer := makeMockedTimer(testCase.clock)

			onTimeout := func() {
				order <- struct{}{}
				wg.Done()
				testCase.advanceTime(testCase.clock)
			}

			ts := NewTimeoutScheduler(onTimeout, ps)
			ts.newTimer = newTimer

			for i := 0; i < testCase.expectedInvocationCount; i++ {
				ts.RegisterTimeout(time.Hour)
				<-order
			}

			wg.Wait()
		})
	}
}

func TestTimeoutSchedulerConcurrentRegister(*testing.T) {
	// Not enough invocations means the test would stall.
	// Too many invocations means a negative counter panic.

	clock := make(chan time.Time, 2)
	newTimer := makeMockedTimer(clock)

	var wg sync.WaitGroup
	wg.Add(1)

	preemptChan := make(<-chan struct{})

	ts := NewTimeoutScheduler(wg.Done, preemptChan)
	ts.newTimer = newTimer

	ts.RegisterTimeout(time.Hour) // First timeout is registered
	ts.RegisterTimeout(time.Hour) // Second should not

	// Clock ticks are after registering, in order to ensure onTimeout() isn't fired until second registration is invoked.
	clock <- time.Now()
	clock <- time.Now()

	wg.Wait()
}

func makeMockedTimer(clock chan time.Time) func(time.Duration) *time.Timer {
	return func(time.Duration) *time.Timer {
		// We use a duration of 0 to not leave a lingering timer
		// after the test finishes.
		// Then we replace the time channel to have control over the timer.
		timer := time.NewTimer(0)
		timer.C = clock
		return timer
	}
}
