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

func TestTimer(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	defer wg.Wait()

	timer := NewTimer(wg.Done)
	go timer.Dispatch()

	timer.SetTimeoutIn(time.Millisecond)
}

func TestTimerCancel(t *testing.T) {
	fatalErrors := make(chan error)
	wg := sync.WaitGroup{}
	wg.Add(1)

	failTimer := NewTimer(func() {
		fatalErrors <- fmt.Errorf("Timer should have been canceled before being called")
	})
	cancelTimer := NewTimer(func() {
		defer wg.Done()
		failTimer.Cancel()
	})

	go failTimer.Dispatch()
	go cancelTimer.Dispatch()

	failTimer.SetTimeoutIn(20 * time.Millisecond)
	cancelTimer.SetTimeoutIn(time.Millisecond)

	wg.Wait()
	// Sleep for 25ms, so that if cancellation does not work the timer will
	// go off and cause the test to fail.
	time.Sleep(25 * time.Millisecond)

	select {
	case err := <-fatalErrors:
		close(fatalErrors)
		assert.NoError(t, err)
	default:
	}
}
