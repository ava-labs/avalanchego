// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSingleStagedTimer(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	ticks := 1
	i := 0
	timer := NewStagedTimer(func() (time.Duration, bool) {
		defer wg.Done()
		i++
		return 0, false
	})
	go timer.Dispatch()

	timer.SetTimeoutIn(time.Millisecond)
	wg.Wait()
	assert.Equal(t, i, ticks)
}

func TestMultiStageTimer(t *testing.T) {
	wg := sync.WaitGroup{}
	ticks := 3
	wg.Add(ticks)

	i := 0
	timer := NewStagedTimer(func() (time.Duration, bool) {
		defer wg.Done()
		i++
		return time.Millisecond, i < ticks
	})
	go timer.Dispatch()

	timer.SetTimeoutIn(time.Millisecond)
	wg.Wait()
	assert.Equal(t, i, ticks)
}

func TestCancelSimpleStagedStimer(t *testing.T) {
	fatalErrors := make(chan error)
	wg := sync.WaitGroup{}
	wg.Add(1)

	msTimer := NewStagedTimer(func() (time.Duration, bool) {
		fatalErrors <- fmt.Errorf("Timer should have been canceled before being called")
		return 0, false
	})
	cancelTimer := NewTimer(func() {
		defer wg.Done()
		msTimer.Cancel()
	})

	go msTimer.Dispatch()
	go cancelTimer.Dispatch()

	// Set [msTimer] with a larger timeout than [cancelTimer]. This should cause
	// [msTimer] to be cancelled before being called.
	msTimer.SetTimeoutIn(20 * time.Millisecond)
	cancelTimer.SetTimeoutIn(time.Millisecond)

	wg.Wait()
	// Sleep for 25 milliseconds, so that if cancellation was not successful
	// the timeout will fire and cause the test to fail.
	time.Sleep(25 * time.Millisecond)

	select {
	case err := <-fatalErrors:
		close(fatalErrors)
		assert.NoError(t, err)
	default:
	}
}

func TestCancelStagedTimer(t *testing.T) {
	fatalErrors := make(chan error)
	wg := sync.WaitGroup{}
	// Add 2 to waitgroup, as we expect one tick from the [msTimer] and
	// a second tick from [cancelTimer].
	wg.Add(2)
	i := 0

	// Set [msTimer] to go off successfully once, and fail on the second
	// callback.
	msTimer := NewStagedTimer(func() (time.Duration, bool) {
		if i > 0 {
			fatalErrors <- fmt.Errorf("Timer should have been canceled before being called a second time")
			return 0, false // no need to call it again
		}
		defer wg.Done()
		i++
		return 20 * time.Millisecond, true
	})
	cancelTimer := NewTimer(func() {
		defer wg.Done()
		msTimer.Cancel()
	})

	go msTimer.Dispatch()
	go cancelTimer.Dispatch()

	// Set [msTimer] with a shorter timeout, so it fires once and then [cancelTimer]
	// should stop the second callback.
	msTimer.SetTimeoutIn(time.Millisecond)
	cancelTimer.SetTimeoutIn(10 * time.Millisecond)

	wg.Wait()
	// Sleep for 25 milliseconds, so that if cancellation was not successful
	// the timeout will fire and cause the test to fail.
	time.Sleep(25 * time.Millisecond)
	select {
	case err := <-fatalErrors:
		close(fatalErrors)
		assert.NoError(t, err)
	default:
	}
}
