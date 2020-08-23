// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"
)

func TestTimer(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	defer wg.Wait()

	timer := NewTimer(wg.Done)
	go timer.Dispatch()

	timer.SetTimeoutIn(time.Millisecond)
}

func TestTimerCancel(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	defer wg.Wait()

	failTimer := NewTimer(func() {
		wg.Done()
		t.Fatalf("Timer should have been canceled before being called")
	})
	cancelTimer := NewTimer(func() {
		wg.Done()
		failTimer.Cancel()
	})

	go failTimer.Dispatch()
	go cancelTimer.Dispatch()

	failTimer.SetTimeoutIn(time.Second)
	cancelTimer.SetTimeoutIn(time.Millisecond)
}
