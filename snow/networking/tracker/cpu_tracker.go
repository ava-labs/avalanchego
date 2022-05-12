// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const epsilon = 1e-9

var _ TimeTracker = &cpuTracker{}

// TimeTracker is an interface for tracking peers' usage of CPU Time
type TimeTracker interface {
	// Registers that the given node started using a CPU at the given time, and
	// that the given portion of the CPU usage should be attributed to the
	// at-large CPU allocation.
	IncCPU(ids.NodeID, time.Time, float64)
	// Registers that the given node stopped using a CPU at the given time, and
	// that the given portion of the CPU usage should be attributed to the
	// at-large CPU allocation.
	DecCPU(ids.NodeID, time.Time, float64)
	// Returns the current EWMA of CPU utilization for the given node.
	Utilization(ids.NodeID, time.Time) float64
	// Returns the current EWMA of CPU utilization by all nodes attributed to
	// the at-large CPU allocation.
	CumulativeAtLargeUtilization(time.Time) float64
	// Returns the duration between [now] and when the CPU utilization of
	// [nodeID] reaches [value], assuming that the node uses no more CPU.
	// If the node's CPU utilization isn't known, or is already <= [value],
	// returns the zero duration.
	TimeUntilUtilization(nodeID ids.NodeID, now time.Time, value float64) time.Duration
}

type cpuTracker struct {
	lock sync.RWMutex

	factory meter.Factory
	// Tracks total CPU usage by all nodes.
	cumulativeMeter meter.Meter
	// Tracks CPU usage by all nodes attributed
	// to the at-large CPU allocation.
	cumulativeAtLargeMeter meter.Meter
	halflife               time.Duration
	// Each element is a meter that tracks total CPU usage by a node.
	// meters is ordered by the last time that a meters was utilized. This
	// doesn't necessarily result in the meters being sorted based on their
	// current utilization. However, in practice the nodes that are not being
	// utilized will move towards the oldest elements where they can be deleted.
	meters  linkedhashmap.LinkedHashmap
	metrics *trackerMetrics
}

func NewCPUTracker(
	reg prometheus.Registerer,
	factory meter.Factory,
	halflife time.Duration,
) (TimeTracker, error) {
	t := &cpuTracker{
		factory:                factory,
		cumulativeMeter:        factory.New(halflife),
		cumulativeAtLargeMeter: factory.New(halflife),
		halflife:               halflife,
		meters:                 linkedhashmap.New(),
	}
	var err error
	t.metrics, err = newCPUTrackerMetrics("cpu_tracker", reg)
	if err != nil {
		return nil, fmt.Errorf("initializing cpuTracker metrics errored with: %w", err)
	}
	return t, nil
}

// getMeter returns the meter used to measure CPU time spent processing
// messages from [nodeID].
// assumes [ct.lock] is held.
func (ct *cpuTracker) getMeter(nodeID ids.NodeID) meter.Meter {
	m, exists := ct.meters.Get(nodeID)
	if exists {
		return m.(meter.Meter)
	}

	newMeter := ct.factory.New(ct.halflife)
	ct.meters.Put(nodeID, newMeter)
	return newMeter
}

func (ct *cpuTracker) IncCPU(
	nodeID ids.NodeID,
	startTime time.Time,
	atLargePortion float64,
) {
	ct.lock.Lock()
	defer func() {
		ct.metrics.cumulativeAtLargeMetric.Set(ct.cumulativeAtLargeMeter.Read(startTime))
		ct.metrics.cumulativeMetric.Set(ct.cumulativeMeter.Read(startTime))
		ct.lock.Unlock()
	}()

	meter := ct.getMeter(nodeID)
	meter.Inc(startTime, 1)
	ct.cumulativeMeter.Inc(startTime, 1)
	ct.cumulativeAtLargeMeter.Inc(startTime, atLargePortion)
}

func (ct *cpuTracker) DecCPU(
	nodeID ids.NodeID,
	endTime time.Time,
	atLargePortion float64,
) {
	ct.lock.Lock()
	defer func() {
		ct.metrics.cumulativeAtLargeMetric.Set(ct.cumulativeAtLargeMeter.Read(endTime))
		ct.metrics.cumulativeMetric.Set(ct.cumulativeMeter.Read(endTime))
		ct.lock.Unlock()
	}()

	meter := ct.getMeter(nodeID)
	meter.Dec(endTime, 1)
	ct.cumulativeMeter.Dec(endTime, 1)
	ct.cumulativeAtLargeMeter.Dec(endTime, atLargePortion)
}

func (ct *cpuTracker) Utilization(nodeID ids.NodeID, now time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(now)

	m, exists := ct.meters.Get(nodeID)
	if !exists {
		return 0
	}
	return m.(meter.Meter).Read(now)
}

func (ct *cpuTracker) CumulativeAtLargeUtilization(now time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(now)
	return ct.cumulativeAtLargeMeter.Read(now)
}

func (ct *cpuTracker) TimeUntilUtilization(
	nodeID ids.NodeID,
	now time.Time,
	value float64,
) time.Duration {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(now)

	m, exists := ct.meters.Get(nodeID)
	if !exists {
		return 0
	}
	return m.(meter.Meter).TimeUntil(now, value)
}

// prune attempts to remove cpu meters that currently show a value less than
// [epsilon].
//
// Because [ct.meters] isn't guaranteed to be sorted by their values, this
// doesn't guarantee that all meters showing less than [epsilon] are removed.
func (ct *cpuTracker) prune(now time.Time) {
	for {
		oldest, meterIntf, exists := ct.meters.Oldest()
		if !exists {
			return
		}
		meter := meterIntf.(meter.Meter)
		if meter.Read(now) > epsilon {
			return
		}
		ct.meters.Delete(oldest)
	}
}

type trackerMetrics struct {
	cumulativeMetric        prometheus.Gauge
	cumulativeAtLargeMetric prometheus.Gauge
}

func newCPUTrackerMetrics(namespace string, reg prometheus.Registerer) (*trackerMetrics, error) {
	m := &trackerMetrics{
		cumulativeMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cumulative_utilization",
			Help:      "Estimated CPU utilization over all nodes. Value should be in [0, number of CPU cores], but can go higher due to overestimation",
		}),
		cumulativeAtLargeMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cumulative_at_large_utilization",
			Help:      "Estimated CPU utilization attributed to the at-large CPU allocation over all nodes. Value should be in [0, number of CPU cores], but can go higher due to overestimation",
		}),
	}
	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.cumulativeMetric),
		reg.Register(m.cumulativeAtLargeMetric),
	)
	return m, errs.Err
}
