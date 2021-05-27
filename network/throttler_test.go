package network

import (
	"fmt"
	"github.com/stretchr/testify/assert"
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
	assert.Equal(t, (3*time.Second)+(1*time.Millisecond), attempt0Duration)
	attempt1Duration := policy.GetBackoffDuration(1)
	assert.Equal(t, (3*time.Second)+(5*time.Second), attempt1Duration)
}

func TestAcquireLock(t *testing.T) {
	thr := NewStaticBackoffThrottler(3, time.Duration(2)*time.Second)
	t1 := time.Now()
	fmt.Println("Acquire quick!")
	thr.Acquire()
	fmt.Println("Acquire quick!")
	thr.Acquire()
	fmt.Println("Acquire quick!")
	thr.Acquire()
	t2 := time.Now()

	thr = NewStaticBackoffThrottler(2, time.Duration(2)*time.Second)
	t1 = time.Now()
	fmt.Println("Acquire quick!")
	thr.Acquire()
	fmt.Println("Acquire quick!")
	thr.Acquire()

	fmt.Println("Acquire wait...")
	thr.Acquire()
	fmt.Println("Acquire wait...")
	thr.Acquire()
	t2 = time.Now()

	delayedDuration := t2.Sub(t1)

	assert.Greater(t, delayedDuration, time.Duration(2)*time.Second)

	time.Sleep(2 * time.Second)

	t1 = time.Now()
	fmt.Println("Acquire quick!")
	thr.Acquire()
	fmt.Println("Acquire quick!")
	thr.Acquire()
	t2 = time.Now()

	finalDuration := t2.Sub(t1)

	assert.Greater(t, delayedDuration, finalDuration)
}
