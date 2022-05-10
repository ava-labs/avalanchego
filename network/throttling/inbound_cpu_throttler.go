// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"golang.org/x/net/context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const epsilon = time.Millisecond

var (
	_ CPUThrottler = &cpuThrottler{}
	_ CPUThrottler = &noCPUThrottler{}
)

// CPUThrottler rate-limits based on the CPU usage caused by each peer.
// We will not read messages from peers whose messages cause excessive
// CPU usage until the CPU usage caused by the peer drops to an acceptable level.
type CPUThrottler interface {
	// Blocks until we can read a message from the given peer.
	// If [ctx] is canceled, returns immediately.
	Acquire(ctx context.Context, nodeID ids.NodeID)
}

// A CPU throttler that always immediately returns on [Acquire].
type noCPUThrottler struct{}

func (t *noCPUThrottler) Acquire(context.Context, ids.NodeID) {}

type CPUThrottlerConfig struct {
	Clock mockable.Clock `json:"-"`
	// The maximum amount of time we'll wait before
	// re-checking whether a call to [Acquire] can return.
	MaxRecheckDelay time.Duration `json:"maxRecheckDelay"`
}

type cpuThrottler struct {
	CPUThrottlerConfig
	metrics *cpuThrottlerMetrics
	// Tells us the target CPU utilization of each node.
	cpuTargeter tracker.CPUTargeter
	// Tells us CPU utilization of each node.
	cpuTracker tracker.TimeTracker
}

type cpuThrottlerMetrics struct {
	totalWaits      prometheus.Counter
	totalNoWaits    prometheus.Counter
	awaitingAcquire prometheus.Gauge
}

func newCPUThrottlerMetrics(namespace string, reg prometheus.Registerer) (*cpuThrottlerMetrics, error) {
	m := &cpuThrottlerMetrics{
		totalWaits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cpu_throttler_total_waits",
			Help:      "Number of times we've waited to read a message from a node because their CPU usage was too high",
		}),
		totalNoWaits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cpu_throttler_total_no_waits",
			Help:      "Number of times we didn't wait to read a message due to their CPU usage being too high",
		}),
		awaitingAcquire: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cpu_throttler_awaiting_acquire",
			Help:      "Number of nodes we're waiting to read a message from because their CPU usage is too high",
		}),
	}
	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.totalWaits),
		reg.Register(m.totalNoWaits),
		reg.Register(m.awaitingAcquire),
	)
	return m, errs.Err
}

func NewCPUThrottler(
	namespace string,
	reg prometheus.Registerer,
	config CPUThrottlerConfig,
	vdrs validators.Set,
	cpuTracker tracker.TimeTracker,
	cpuTargeter tracker.CPUTargeter,
) (CPUThrottler, error) {
	metrics, err := newCPUThrottlerMetrics(namespace, reg)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize CPU throttler metrics: %w", err)
	}
	return &cpuThrottler{
		metrics:            metrics,
		CPUThrottlerConfig: config,
		cpuTargeter:        cpuTargeter,
		cpuTracker:         cpuTracker,
	}, nil
}

func (t *cpuThrottler) Acquire(ctx context.Context, nodeID ids.NodeID) {
	// Fires when we should re-check whether this node's CPU usage
	// has fallen to an acceptable level.
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	// [waited] is true if we waited for this node's CPU usage
	// to fall to an acceptable level before returning
	waited := false
	// Note that we increment [numWaiting] here even though
	// we might not actually wait. In this case, [numWaiting]
	// will be decremented pretty much immediately.
	// Technically this causes this metric to be incorrect for
	// a small duration, but doing it like this makes the code cleaner.
	t.metrics.awaitingAcquire.Inc()
	defer func() {
		t.metrics.awaitingAcquire.Dec()
		if waited {
			t.metrics.totalWaits.Inc()
		} else {
			t.metrics.totalNoWaits.Inc()
		}
	}()

	for {
		now := t.Clock.Time()
		// Get target CPU usage for this node.
		targetCPU := t.cpuTargeter.TargetCPUUsage(nodeID)
		// Get actual CPU usage for this node.
		actualCPU := t.cpuTracker.Utilization(nodeID, now)
		if actualCPU <= targetCPU {
			return
		}
		// See how long it will take for actual CPU usage to drop to target,
		// assuming this node uses no more CPU.
		waitDuration := t.cpuTracker.TimeUntilUtilization(nodeID, now, targetCPU)
		if waitDuration < epsilon {
			// If the amount of time until we reach the CPU target is very small,
			// just return to avoid a situation where we excessively re-check.
			return
		}
		if waitDuration > t.MaxRecheckDelay {
			// Re-check at least every [t.MaxRecheckDelay] in case it will be a
			// very long time until CPU usage reaches the target level.
			//
			// Note that not only can a node's CPU usage decrease over time, but
			// also its target CPU usage may increase.
			// In this case, the node's CPU usage can drop to the target level
			// sooner than [waitDuration] because the target has increased.
			// The minimum re-check frequency accounts for that case by
			// optimistically re-checking whether the node's CPU usage is now at
			// an acceptable level.
			waitDuration = t.MaxRecheckDelay
		}
		waited = true
		timer.Reset(waitDuration)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
