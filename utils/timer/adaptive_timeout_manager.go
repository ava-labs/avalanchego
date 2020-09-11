// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"container/heap"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
)

type adaptiveTimeout struct {
	index    int           // Index in the wait queue
	id       ids.ID        // Unique ID of this timeout
	handler  func()        // Function to execute if timed out
	duration time.Duration // How long this timeout was set for
	deadline time.Time     // When this timeout should be fired
}

// A timeoutQueue implements heap.Interface and holds adaptiveTimeouts.
type timeoutQueue []*adaptiveTimeout

func (tq timeoutQueue) Len() int           { return len(tq) }
func (tq timeoutQueue) Less(i, j int) bool { return tq[i].deadline.Before(tq[j].deadline) }
func (tq timeoutQueue) Swap(i, j int) {
	tq[i], tq[j] = tq[j], tq[i]
	tq[i].index = i
	tq[j].index = j
}

// Push adds an item to this priority queue. x must have type *adaptiveTimeout
func (tq *timeoutQueue) Push(x interface{}) {
	item := x.(*adaptiveTimeout)
	item.index = len(*tq)
	*tq = append(*tq, item)
}

// Pop returns the next item in this queue
func (tq *timeoutQueue) Pop() interface{} {
	n := len(*tq)
	item := (*tq)[n-1]
	(*tq)[n-1] = nil // make sure the item is freed from memory
	*tq = (*tq)[:n-1]
	return item
}

// AdaptiveTimeoutConfig contains the parameters that should be provided to the
// adaptive timeout manager.
type AdaptiveTimeoutConfig struct {
	InitialTimeout    time.Duration
	MinimumTimeout    time.Duration
	MaximumTimeout    time.Duration
	TimeoutMultiplier float64
	TimeoutReduction  time.Duration

	Namespace  string
	Registerer prometheus.Registerer
}

// AdaptiveTimeoutManager is a manager for timeouts.
type AdaptiveTimeoutManager struct {
	currentDurationMetric prometheus.Gauge

	minimumTimeout    time.Duration
	maximumTimeout    time.Duration
	timeoutMultiplier float64
	timeoutReduction  time.Duration

	lock           sync.Mutex
	currentTimeout time.Duration // Amount of time before a timeout
	timeoutMap     map[[32]byte]*adaptiveTimeout
	timeoutQueue   timeoutQueue
	timer          *Timer // Timer that will fire to clear the timeouts
}

// Initialize this timeout manager with the provided config
func (tm *AdaptiveTimeoutManager) Initialize(config *AdaptiveTimeoutConfig) error {
	tm.currentDurationMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: config.Namespace,
		Name:      "network_timeout",
		Help:      "Duration of current network timeouts in nanoseconds",
	})
	tm.minimumTimeout = config.MinimumTimeout
	tm.maximumTimeout = config.MaximumTimeout
	tm.timeoutMultiplier = config.TimeoutMultiplier
	tm.timeoutReduction = config.TimeoutReduction
	tm.currentTimeout = config.InitialTimeout
	tm.timeoutMap = make(map[[32]byte]*adaptiveTimeout)
	tm.timer = NewTimer(tm.Timeout)
	return config.Registerer.Register(tm.currentDurationMetric)
}

// Dispatch ...
func (tm *AdaptiveTimeoutManager) Dispatch() { tm.timer.Dispatch() }

// Stop executing timeouts
func (tm *AdaptiveTimeoutManager) Stop() { tm.timer.Stop() }

// Put puts hash into the hash map
func (tm *AdaptiveTimeoutManager) Put(id ids.ID, handler func()) time.Time {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	return tm.put(id, handler)
}

// Remove the item that no longer needs to be there.
func (tm *AdaptiveTimeoutManager) Remove(id ids.ID) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	currentTime := time.Now()

	tm.remove(id, currentTime)
}

// Timeout registers a timeout
func (tm *AdaptiveTimeoutManager) Timeout() {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.timeout()
}

func (tm *AdaptiveTimeoutManager) timeout() {
	currentTime := time.Now()
	// removeExpiredHead returns nil once there is nothing left to remove
	for {
		timeout := tm.removeExpiredHead(currentTime)
		if timeout == nil {
			break
		}

		// Don't execute a callback with a lock held
		tm.lock.Unlock()
		timeout()
		tm.lock.Lock()
	}
	tm.registerTimeout()
}

func (tm *AdaptiveTimeoutManager) put(id ids.ID, handler func()) time.Time {
	currentTime := time.Now()
	tm.remove(id, currentTime)

	timeout := &adaptiveTimeout{
		id:       id,
		handler:  handler,
		duration: tm.currentTimeout,
		deadline: currentTime.Add(tm.currentTimeout),
	}
	tm.timeoutMap[id.Key()] = timeout
	heap.Push(&tm.timeoutQueue, timeout)

	tm.registerTimeout()
	return timeout.deadline
}

func (tm *AdaptiveTimeoutManager) remove(id ids.ID, currentTime time.Time) {
	key := id.Key()
	timeout, exists := tm.timeoutMap[key]
	if !exists {
		return
	}

	if timeout.deadline.Before(currentTime) {
		// This request is being removed because it timed out.
		if timeout.duration >= tm.currentTimeout {
			// If the current timeout duration is less than or equal to the
			// timeout that was triggered, double the duration.
			tm.currentTimeout = time.Duration(float64(tm.currentTimeout) * tm.timeoutMultiplier)

			if tm.currentTimeout > tm.maximumTimeout {
				// Make sure that we never get stuck in a bad situation
				tm.currentTimeout = tm.maximumTimeout
			}
		}
	} else {
		// This request is being removed because it finished successfully.
		if timeout.duration <= tm.currentTimeout {
			// If the current timeout duration is greater than or equal to the
			// timeout that was fullfilled, reduce future timeouts.
			tm.currentTimeout -= tm.timeoutReduction

			if tm.currentTimeout < tm.minimumTimeout {
				// Make sure that we never get stuck in a bad situation
				tm.currentTimeout = tm.minimumTimeout
			}
		}
	}

	// Make sure the metrics report the current timeouts
	tm.currentDurationMetric.Set(float64(tm.currentTimeout))

	// Remove the timeout from the map
	delete(tm.timeoutMap, key)

	// Remove the timeout from the queue
	heap.Remove(&tm.timeoutQueue, timeout.index)
}

// Returns true if the head was removed, false otherwise
func (tm *AdaptiveTimeoutManager) removeExpiredHead(currentTime time.Time) func() {
	if tm.timeoutQueue.Len() == 0 {
		return nil
	}

	nextTimeout := tm.timeoutQueue[0]
	if nextTimeout.deadline.After(currentTime) {
		return nil
	}
	tm.remove(nextTimeout.id, currentTime)
	return nextTimeout.handler
}

func (tm *AdaptiveTimeoutManager) registerTimeout() {
	if tm.timeoutQueue.Len() == 0 {
		// There are no pending timeouts
		tm.timer.Cancel()
		return
	}

	currentTime := time.Now()
	nextTimeout := tm.timeoutQueue[0]
	timeToNextTimeout := nextTimeout.deadline.Sub(currentTime)
	tm.timer.SetTimeoutIn(timeToNextTimeout)
}
