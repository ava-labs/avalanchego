// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"container/heap"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errNonPositiveHalflife = errors.New("timeout halflife must be positive")

	_ heap.Interface         = (*timeoutQueue)(nil)
	_ AdaptiveTimeoutManager = (*adaptiveTimeoutManager)(nil)
)

type adaptiveTimeout struct {
	index          int           // Index in the wait queue
	id             ids.RequestID // Unique ID of this timeout
	handler        func()        // Function to execute if timed out
	duration       time.Duration // How long this timeout was set for
	deadline       time.Time     // When this timeout should be fired
	measureLatency bool          // Whether this request should impact latency
}

type timeoutQueue []*adaptiveTimeout

func (tq timeoutQueue) Len() int {
	return len(tq)
}

func (tq timeoutQueue) Less(i, j int) bool {
	return tq[i].deadline.Before(tq[j].deadline)
}

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

// AdaptiveTimeoutConfig contains the parameters provided to the
// adaptive timeout manager.
type AdaptiveTimeoutConfig struct {
	InitialTimeout time.Duration `json:"initialTimeout"`
	MinimumTimeout time.Duration `json:"minimumTimeout"`
	MaximumTimeout time.Duration `json:"maximumTimeout"`
	// Timeout is [timeoutCoefficient] * average response time
	// [timeoutCoefficient] must be > 1
	TimeoutCoefficient float64 `json:"timeoutCoefficient"`
	// Larger halflife --> less volatile timeout
	// [timeoutHalfLife] must be positive
	TimeoutHalflife time.Duration `json:"timeoutHalflife"`
}

type AdaptiveTimeoutManager interface {
	// Start the timeout manager.
	// Must be called before any other method.
	// Must only be called once.
	Dispatch()
	// Stop the timeout manager.
	// Must only be called once.
	Stop()
	// Returns the current network timeout duration.
	TimeoutDuration() time.Duration
	// Registers a timeout for the item with the given [id].
	// If the timeout occurs before the item is Removed, [timeoutHandler] is called.
	Put(id ids.RequestID, measureLatency bool, timeoutHandler func())
	// Remove the timeout associated with [id].
	// Its timeout handler will not be called.
	Remove(id ids.RequestID)
	// ObserveLatency manually registers a response latency.
	// We use this to pretend that it a query to a benched validator
	// timed out when actually, we never even sent them a request.
	ObserveLatency(latency time.Duration)
}

type adaptiveTimeoutManager struct {
	lock sync.Mutex
	// Tells the time. Can be faked for testing.
	clock                            mockable.Clock
	networkTimeoutMetric, avgLatency prometheus.Gauge
	numTimeouts                      prometheus.Counter
	numPendingTimeouts               prometheus.Gauge
	// Averages the response time from all peers
	averager math.Averager
	// Timeout is [timeoutCoefficient] * average response time
	// [timeoutCoefficient] must be > 1
	timeoutCoefficient float64
	minimumTimeout     time.Duration
	maximumTimeout     time.Duration
	currentTimeout     time.Duration // Amount of time before a timeout
	timeoutMap         map[ids.RequestID]*adaptiveTimeout
	timeoutQueue       timeoutQueue
	timer              *Timer // Timer that will fire to clear the timeouts
}

func NewAdaptiveTimeoutManager(
	config *AdaptiveTimeoutConfig,
	metricsNamespace string,
	metricsRegister prometheus.Registerer,
) (AdaptiveTimeoutManager, error) {
	switch {
	case config.InitialTimeout > config.MaximumTimeout:
		return nil, fmt.Errorf("initial timeout (%s) > maximum timeout (%s)", config.InitialTimeout, config.MaximumTimeout)
	case config.InitialTimeout < config.MinimumTimeout:
		return nil, fmt.Errorf("initial timeout (%s) < minimum timeout (%s)", config.InitialTimeout, config.MinimumTimeout)
	case config.TimeoutCoefficient < 1:
		return nil, fmt.Errorf("timeout coefficient must be >= 1 but got %f", config.TimeoutCoefficient)
	case config.TimeoutHalflife <= 0:
		return nil, errNonPositiveHalflife
	}

	tm := &adaptiveTimeoutManager{
		networkTimeoutMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "current_timeout",
			Help:      "Duration of current network timeout in nanoseconds",
		}),
		avgLatency: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "average_latency",
			Help:      "Average network latency in nanoseconds",
		}),
		numTimeouts: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "timeouts",
			Help:      "Number of timed out requests",
		}),
		numPendingTimeouts: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "pending_timeouts",
			Help:      "Number of pending timeouts",
		}),
		minimumTimeout:     config.MinimumTimeout,
		maximumTimeout:     config.MaximumTimeout,
		currentTimeout:     config.InitialTimeout,
		timeoutCoefficient: config.TimeoutCoefficient,
		timeoutMap:         make(map[ids.RequestID]*adaptiveTimeout),
	}
	tm.timer = NewTimer(tm.timeout)
	tm.averager = math.NewAverager(float64(config.InitialTimeout), config.TimeoutHalflife, tm.clock.Time())

	errs := &wrappers.Errs{}
	errs.Add(
		metricsRegister.Register(tm.networkTimeoutMetric),
		metricsRegister.Register(tm.avgLatency),
		metricsRegister.Register(tm.numTimeouts),
		metricsRegister.Register(tm.numPendingTimeouts),
	)
	return tm, errs.Err
}

func (tm *adaptiveTimeoutManager) TimeoutDuration() time.Duration {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	return tm.currentTimeout
}

func (tm *adaptiveTimeoutManager) Dispatch() {
	tm.timer.Dispatch()
}

func (tm *adaptiveTimeoutManager) Stop() {
	tm.timer.Stop()
}

func (tm *adaptiveTimeoutManager) Put(id ids.RequestID, measureLatency bool, timeoutHandler func()) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.put(id, measureLatency, timeoutHandler)
}

// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) put(id ids.RequestID, measureLatency bool, handler func()) {
	now := tm.clock.Time()
	tm.remove(id, now)

	timeout := &adaptiveTimeout{
		id:             id,
		handler:        handler,
		duration:       tm.currentTimeout,
		deadline:       now.Add(tm.currentTimeout),
		measureLatency: measureLatency,
	}
	tm.timeoutMap[id] = timeout
	tm.numPendingTimeouts.Set(float64(len(tm.timeoutMap)))
	heap.Push(&tm.timeoutQueue, timeout)

	tm.setNextTimeoutTime()
}

func (tm *adaptiveTimeoutManager) Remove(id ids.RequestID) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.remove(id, tm.clock.Time())
}

// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) remove(id ids.RequestID, now time.Time) {
	timeout, exists := tm.timeoutMap[id]
	if !exists {
		return
	}

	// Observe the response time to update average network response time.
	if timeout.measureLatency {
		timeoutRegisteredAt := timeout.deadline.Add(-1 * timeout.duration)
		latency := now.Sub(timeoutRegisteredAt)
		tm.observeLatencyAndUpdateTimeout(latency, now)
	}

	// Remove the timeout from the map
	delete(tm.timeoutMap, id)
	tm.numPendingTimeouts.Set(float64(len(tm.timeoutMap)))

	// Remove the timeout from the queue
	heap.Remove(&tm.timeoutQueue, timeout.index)
}

// Assumes [tm.lock] is not held.
func (tm *adaptiveTimeoutManager) timeout() {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	now := tm.clock.Time()
	for {
		// getNextTimeoutHandler returns nil once there is nothing left to remove
		timeoutHandler := tm.getNextTimeoutHandler(now)
		if timeoutHandler == nil {
			break
		}
		tm.numTimeouts.Inc()

		// Don't execute a callback with a lock held
		tm.lock.Unlock()
		timeoutHandler()
		tm.lock.Lock()
	}
	tm.setNextTimeoutTime()
}

func (tm *adaptiveTimeoutManager) ObserveLatency(latency time.Duration) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.observeLatencyAndUpdateTimeout(latency, tm.clock.Time())
}

// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) observeLatencyAndUpdateTimeout(latency time.Duration, now time.Time) {
	tm.averager.Observe(float64(latency), now)
	avgLatency := tm.averager.Read()
	tm.currentTimeout = time.Duration(tm.timeoutCoefficient * avgLatency)
	if tm.currentTimeout > tm.maximumTimeout {
		tm.currentTimeout = tm.maximumTimeout
	} else if tm.currentTimeout < tm.minimumTimeout {
		tm.currentTimeout = tm.minimumTimeout
	}
	// Update the metrics
	tm.networkTimeoutMetric.Set(float64(tm.currentTimeout))
	tm.avgLatency.Set(avgLatency)
}

// Returns the handler function associated with the next timeout.
// If there are no timeouts, or if the next timeout is after [now],
// returns nil.
// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) getNextTimeoutHandler(now time.Time) func() {
	if tm.timeoutQueue.Len() == 0 {
		return nil
	}

	nextTimeout := tm.timeoutQueue[0]
	if nextTimeout.deadline.After(now) {
		return nil
	}
	tm.remove(nextTimeout.id, now)
	return nextTimeout.handler
}

// Calculate the time of the next timeout and set
// the timer to fire at that time.
func (tm *adaptiveTimeoutManager) setNextTimeoutTime() {
	if tm.timeoutQueue.Len() == 0 {
		// There are no pending timeouts
		tm.timer.Cancel()
		return
	}

	now := tm.clock.Time()
	nextTimeout := tm.timeoutQueue[0]
	timeToNextTimeout := nextTimeout.deadline.Sub(now)
	tm.timer.SetTimeoutIn(timeToNextTimeout)
}
