package network

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestAcquireLock(t *testing.T) {
	thr := NewThrottler(3)
	t1 := time.Now()
	thr.Acquire()
	thr.Acquire()
	thr.Acquire()
	t2 := time.Now()

	fastestDuration := t2.Sub(t1)

	thr = NewThrottler(2)
	t1 = time.Now()
	thr.Acquire()
	thr.Acquire()
	thr.Acquire()
	t2 = time.Now()

	delayedDuration := t2.Sub(t1)

	assert.Greater(t, delayedDuration, fastestDuration)

	time.Sleep(2 * time.Second)

	t1 = time.Now()
	thr.Acquire()
	thr.Acquire()
	t2 = time.Now()

	finalDuration := t2.Sub(t1)

	assert.Greater(t, delayedDuration, finalDuration)
}
