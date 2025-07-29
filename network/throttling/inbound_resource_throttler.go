// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
)

const epsilon = time.Millisecond

var (
	_ SystemThrottler = (*systemThrottler)(nil)
	_ SystemThrottler = noSystemThrottler{}
)

// SystemThrottler rate-limits based on the system metrics usage caused by each
// peer. We will not read messages from peers whose messages cause excessive
// usage until the usage caused by the peer drops to an acceptable level.
type SystemThrottler interface {
	// Blocks until we can read a message from the given peer.
	// If [ctx] is canceled, returns immediately.
	Acquire(ctx context.Context, nodeID ids.NodeID)
}

// A system throttler that always immediately returns on [Acquire].
type noSystemThrottler struct{}

func (noSystemThrottler) Acquire(context.Context, ids.NodeID) {}

type SystemThrottlerConfig struct {
	Clock mockable.Clock `json:"-"`
	// The maximum amount of time we'll wait before re-checking whether a call
	// to [Acquire] can return.
	MaxRecheckDelay time.Duration `json:"maxRecheckDelay"`
}

type systemThrottler struct {
	SystemThrottlerConfig
	metrics *systemThrottlerMetrics
	// Tells us the target utilization of each node.
	targeter tracker.Targeter
	// Tells us the utilization of each node.
	tracker tracker.Tracker
	// Invariant: [timerPool] only returns timers that have been stopped and drained.
	timerPool sync.Pool
}

type systemThrottlerMetrics struct {
	totalWaits      prometheus.Counter
	totalNoWaits    prometheus.Counter
	awaitingAcquire prometheus.Gauge
}

func newSystemThrottlerMetrics(namespace string, reg prometheus.Registerer) (*systemThrottlerMetrics, error) {
	m := &systemThrottlerMetrics{
		totalWaits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "throttler_total_waits",
			Help:      "Number of times we've waited to read a message from a node because their usage was too high",
		}),
		totalNoWaits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "throttler_total_no_waits",
			Help:      "Number of times we didn't wait to read a message because their usage is too high",
		}),
		awaitingAcquire: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "throttler_awaiting_acquire",
			Help:      "Number of nodes we're waiting to read a message from because their usage is too high",
		}),
	}
	err := errors.Join(
		reg.Register(m.totalWaits),
		reg.Register(m.totalNoWaits),
		reg.Register(m.awaitingAcquire),
	)
	return m, err
}

func NewSystemThrottler(
	namespace string,
	reg prometheus.Registerer,
	config SystemThrottlerConfig,
	tracker tracker.Tracker,
	targeter tracker.Targeter,
) (SystemThrottler, error) {
	metrics, err := newSystemThrottlerMetrics(namespace, reg)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize system throttler metrics: %w", err)
	}
	return &systemThrottler{
		metrics:               metrics,
		SystemThrottlerConfig: config,
		targeter:              targeter,
		tracker:               tracker,
		timerPool: sync.Pool{
			New: func() interface{} {
				// Satisfy invariant that timer is stopped and drained.
				return timerpkg.StoppedTimer()
			},
		},
	}, nil
}

func (t *systemThrottler) Acquire(ctx context.Context, nodeID ids.NodeID) {
	// [timer] fires when we should re-check whether this node's
	// usage has fallen to an acceptable level.
	// Lazily initialize timer only if we actually need to wait.
	var timer *time.Timer
	defer func() {
		if timer != nil { // We waited at least once for usage to fall.
			t.metrics.totalWaits.Inc()
			// Note that [t.metrics.awaitingAcquire.Inc()] was called once if
			// and only if [waited] is true.
			t.metrics.awaitingAcquire.Dec()
		} else {
			t.metrics.totalNoWaits.Inc()
		}
	}()

	for {
		now := t.Clock.Time()
		// Get target usage for this node.
		target := t.targeter.TargetUsage(nodeID)
		// Get actual usage for this node.
		usage := t.tracker.Usage(nodeID, now)
		if usage <= target {
			return
		}
		// See how long it will take for actual usage to drop to the target,
		// assuming this node uses no more resources.
		waitDuration := t.tracker.TimeUntilUsage(nodeID, now, target)
		if waitDuration < epsilon {
			// If the amount of time until we reach the target is very small,
			// just return to avoid a situation where we excessively re-check.
			return
		}
		if waitDuration > t.MaxRecheckDelay {
			// Re-check at least every [t.MaxRecheckDelay] in case it will be a
			// very long time until usage reaches the target level.
			//
			// Note that not only can a node's usage decrease over time, but
			// also its target usage may increase.
			// In this case, the node's usage can drop to the target level
			// sooner than [waitDuration] because the target has increased.
			// The minimum re-check frequency accounts for that case by
			// optimistically re-checking whether the node's usage is now at an
			// acceptable level.
			waitDuration = t.MaxRecheckDelay
		}

		if timer == nil {
			// Note this is called at most once.
			t.metrics.awaitingAcquire.Inc()

			timer = t.timerPool.Get().(*time.Timer)
			defer t.timerPool.Put(timer)
		}

		timer.Reset(waitDuration)
		select {
		case <-ctx.Done():
			// Satisfy [t.timerPool] invariant.
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}
