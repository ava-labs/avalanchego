// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var (
	errNonPositiveHalflife        = errors.New("timeout halflife must be positive")
	errInitialTimeoutAboveMaximum = errors.New("initial timeout cannot be greater than maximum timeout")
	errInitialTimeoutBelowMinimum = errors.New("initial timeout cannot be less than minimum timeout")
	errTooSmallTimeoutCoefficient = errors.New("timeout coefficient must be >= 1")

	_ AdaptiveTimeoutManager = (*adaptiveTimeoutManager)(nil)
)

type adaptiveTimeout struct {
	id             ids.RequestID // Unique ID of this timeout
	handler        func()        // Function to execute if timed out
	duration       time.Duration // How long this timeout was set for
	deadline       time.Time     // When this timeout should be fired
	measureLatency bool          // Whether this request should impact latency
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
	timeoutHeap        heap.Map[ids.RequestID, *adaptiveTimeout]
	timer              *Timer // Timer that will fire to clear the timeouts
}

func NewAdaptiveTimeoutManager(
	config *AdaptiveTimeoutConfig,
	reg prometheus.Registerer,
) (AdaptiveTimeoutManager, error) {
	switch {
	case config.InitialTimeout > config.MaximumTimeout:
		return nil, fmt.Errorf("%w: (%s) > (%s)", errInitialTimeoutAboveMaximum, config.InitialTimeout, config.MaximumTimeout)
	case config.InitialTimeout < config.MinimumTimeout:
		return nil, fmt.Errorf("%w: (%s) < (%s)", errInitialTimeoutBelowMinimum, config.InitialTimeout, config.MinimumTimeout)
	case config.TimeoutCoefficient < 1:
		return nil, fmt.Errorf("%w: %f", errTooSmallTimeoutCoefficient, config.TimeoutCoefficient)
	case config.TimeoutHalflife <= 0:
		return nil, errNonPositiveHalflife
	}

	tm := &adaptiveTimeoutManager{
		networkTimeoutMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "current_timeout",
			Help: "Duration of current network timeout in nanoseconds",
		}),
		avgLatency: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "average_latency",
			Help: "Average network latency in nanoseconds",
		}),
		numTimeouts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "timeouts",
			Help: "Number of timed out requests",
		}),
		numPendingTimeouts: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pending_timeouts",
			Help: "Number of pending timeouts",
		}),
		minimumTimeout:     config.MinimumTimeout,
		maximumTimeout:     config.MaximumTimeout,
		currentTimeout:     config.InitialTimeout,
		timeoutCoefficient: config.TimeoutCoefficient,
		timeoutHeap: heap.NewMap[ids.RequestID, *adaptiveTimeout](func(a, b *adaptiveTimeout) bool {
			return a.deadline.Before(b.deadline)
		}),
	}
	tm.timer = NewTimer(tm.timeout)
	tm.averager = math.NewAverager(float64(config.InitialTimeout), config.TimeoutHalflife, tm.clock.Time())

	err := errors.Join(
		reg.Register(tm.networkTimeoutMetric),
		reg.Register(tm.avgLatency),
		reg.Register(tm.numTimeouts),
		reg.Register(tm.numPendingTimeouts),
	)
	return tm, err
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
	tm.timeoutHeap.Push(id, timeout)
	tm.numPendingTimeouts.Set(float64(tm.timeoutHeap.Len()))

	tm.setNextTimeoutTime()
}

func (tm *adaptiveTimeoutManager) Remove(id ids.RequestID) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.remove(id, tm.clock.Time())
}

// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) remove(id ids.RequestID, now time.Time) {
	// Observe the response time to update average network response time.
	timeout, exists := tm.timeoutHeap.Remove(id)
	if !exists {
		return
	}

	if timeout.measureLatency {
		timeoutRegisteredAt := timeout.deadline.Add(-1 * timeout.duration)
		latency := now.Sub(timeoutRegisteredAt)
		tm.observeLatencyAndUpdateTimeout(latency, now)
	}
	tm.numPendingTimeouts.Set(float64(tm.timeoutHeap.Len()))
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
	_, nextTimeout, ok := tm.timeoutHeap.Peek()
	if !ok {
		return nil
	}
	if nextTimeout.deadline.After(now) {
		return nil
	}
	tm.remove(nextTimeout.id, now)
	return nextTimeout.handler
}

// Calculate the time of the next timeout and set
// the timer to fire at that time.
func (tm *adaptiveTimeoutManager) setNextTimeoutTime() {
	_, nextTimeout, ok := tm.timeoutHeap.Peek()
	if !ok {
		// There are no pending timeouts
		tm.timer.Cancel()
		return
	}

	now := tm.clock.Time()
	timeToNextTimeout := nextTimeout.deadline.Sub(now)
	tm.timer.SetTimeoutIn(timeToNextTimeout)
}
