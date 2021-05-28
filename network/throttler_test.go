package network

import (
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

func TestStaticBackoffPolicy_Backoff(t *testing.T) {
	policy := StaticBackoffPolicy{backoffDuration: 1 * time.Second}
	backoffDuration := policy.GetBackoffDuration()
	if backoffDuration != 1*time.Second {
		t.Fatalf("Expected backoff duration to be static 1 second")
	}
}

func TestIncrementalBackoffPolicy(t *testing.T) {
	policy := IncrementalBackoffPolicy{
		backoffDuration:   3 * time.Second,
		incrementDuration: 5 * time.Second,
	}
	attempt0Duration := policy.GetBackoffDuration(0)
	assert.Equal(t, 3*time.Second, attempt0Duration)
	attempt1Duration := policy.GetBackoffDuration(1)
	assert.Equal(t, (3*time.Second)+(5*time.Second), attempt1Duration)
}

func submitAndWait(fn func(), times int) {
	goFn := func(w *sync.WaitGroup) {
		fn()
		w.Done()
	}

	wg := sync.WaitGroup{}
	wg.Add(times)
	for i := 0; i < times; i++ {
		go goFn(&wg)
	}
	wg.Wait()
}

func TestAcquireLock(t *testing.T) {
	thr := NewStaticBackoffThrottler(3, time.Duration(2)*time.Second)
	t1 := time.Now()
	submitAndWait(thr.Acquire, 3)
	t2 := time.Now()

	// Create throttler with 2 aps limit and static backoff of 1 second
	// We create a waitgroup for 4 actions, submit all 4 using goroutines and wait for them
	// to complete. Since it is 2 actions allowed per second, the total time for all 4
	// concurrent requests should be around 1 second.
	thr = NewStaticBackoffThrottler(2, time.Duration(1)*time.Second)
	submitAndWait(thr.Acquire, 4)
	t2 = time.Now()

	delayedDuration := t2.Sub(t1)

	assert.Greater(t, delayedDuration, 1*time.Second)
	assert.Less(t, delayedDuration, 2*time.Second)

	time.Sleep(2 * time.Second)

	t1 = time.Now()
	submitAndWait(thr.Acquire, 2)
	t2 = time.Now()

	finalDuration := t2.Sub(t1)

	assert.Greater(t, delayedDuration, finalDuration)
}
