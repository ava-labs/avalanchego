package network

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

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
