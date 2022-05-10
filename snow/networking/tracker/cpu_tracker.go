// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/uptime"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const epsilon = 1e-9

var (
	_ TimeTracker                    = &cpuTracker{}
	_ validators.SetCallbackListener = &cpuTracker{}
)

// TimeTracker is an interface for tracking peers' usage of CPU Time
type TimeTracker interface {
	// Registers that the given node started using a CPU
	// core at the given time.
	StartCPU(ids.NodeID, time.Time)
	// Registers that the given node stopped using a CPU
	// core at the given time.
	StopCPU(ids.NodeID, time.Time)
	// Returns the current EWMA of CPU utilization for the given node.
	Utilization(ids.NodeID, time.Time) float64
	// Returns the current EWMA of CPU utilization for all nodes.
	CumulativeUtilization(time.Time) float64
	// Returns the duration between [now] and when the CPU utilization of
	// [nodeID] reaches [value], assuming that the node uses no more CPU.
	// If the node's CPU utilization isn't known, or is already <= [value],
	// returns the zero duration.
	TimeUntilUtilization(nodeID ids.NodeID, now time.Time, value float64) time.Duration
	// Returns the number of nodes that have recently used CPU time.
	Len() int
	// Returns the total weight of CPU spenders that have recently used CPU.
	ActiveWeight() uint64
}

type cpuTracker struct {
	lock sync.RWMutex

	factory         uptime.Factory
	cumulativeMeter uptime.Meter
	halflife        time.Duration
	// cpuSpenders is ordered by the last time that a meter was utilized. This
	// doesn't necessarily result in the meters being sorted based on their
	// current utilization. However, in practice the nodes that are not being
	// utilized will move towards the oldest elements where they can be deleted.
	cpuSpenders linkedhashmap.LinkedHashmap
	// A validator's weight is included in [activeWeight] if and only if
	// the validator is in [cpuSpenders].
	activeWeight uint64
	metrics      *trackerMetrics
	weights      map[ids.NodeID]uint64
}

func NewCPUTracker(reg prometheus.Registerer, factory uptime.Factory, halflife time.Duration, vdrs validators.Set) (TimeTracker, error) {
	t := &cpuTracker{
		factory:         factory,
		cumulativeMeter: factory.New(halflife),
		halflife:        halflife,
		cpuSpenders:     linkedhashmap.New(),
		weights:         map[ids.NodeID]uint64{},
	}
	var err error
	t.metrics, err = newCPUTrackerMetrics("cpuTracker", reg)
	if err != nil {
		return nil, fmt.Errorf("initializing cpuTracker metrics errored with: %w", err)
	}
	vdrs.RegisterCallbackListener(t)
	return t, nil
}

func (ct *cpuTracker) OnValidatorAdded(validatorID ids.NodeID, weight uint64) {
	ct.lock.Lock()
	defer ct.lock.Unlock()
	ct.weights[validatorID] = weight
	if _, exists := ct.cpuSpenders.Get(validatorID); exists {
		ct.activeWeight += weight
		ct.metrics.activeWeightMetric.Set(float64(ct.activeWeight))
	}
}

func (ct *cpuTracker) OnValidatorRemoved(validatorID ids.NodeID, weight uint64) {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	delete(ct.weights, validatorID)
	if _, exists := ct.cpuSpenders.Get(validatorID); exists {
		ct.activeWeight -= weight
		ct.metrics.activeWeightMetric.Set(float64(ct.activeWeight))
	}
}

func (ct *cpuTracker) OnValidatorWeightChanged(validatorID ids.NodeID, oldWeight, newWeight uint64) {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.weights[validatorID] = newWeight
	if _, exists := ct.cpuSpenders.Get(validatorID); exists {
		ct.activeWeight -= oldWeight
		ct.activeWeight += newWeight
		ct.metrics.activeWeightMetric.Set(float64(ct.activeWeight))
	}
}

// getMeter returns the meter used to measure CPU time spent processing
// messages from [nodeID]
// assumes the lock is held
func (ct *cpuTracker) getMeter(nodeID ids.NodeID) uptime.Meter {
	meter, exists := ct.cpuSpenders.Get(nodeID)
	if exists {
		return meter.(uptime.Meter)
	}

	newMeter := ct.factory.New(ct.halflife)
	ct.cpuSpenders.Put(nodeID, newMeter)

	if weight, ok := ct.weights[nodeID]; ok {
		ct.activeWeight += weight
		ct.metrics.activeWeightMetric.Set(float64(ct.activeWeight))
	}

	ct.metrics.activeLenMetric.Set(float64(ct.cpuSpenders.Len()))
	return newMeter
}

func (ct *cpuTracker) StartCPU(nodeID ids.NodeID, startTime time.Time) {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	meter := ct.getMeter(nodeID)
	ct.cumulativeMeter.Start(startTime)
	meter.Start(startTime)
}

func (ct *cpuTracker) StopCPU(nodeID ids.NodeID, endTime time.Time) {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	meter := ct.getMeter(nodeID)
	ct.cumulativeMeter.Stop(endTime)
	meter.Stop(endTime)
}

func (ct *cpuTracker) Utilization(nodeID ids.NodeID, now time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(now)

	meter, exists := ct.cpuSpenders.Get(nodeID)
	if !exists {
		return 0
	}
	return meter.(uptime.Meter).Read(now)
}

func (ct *cpuTracker) CumulativeUtilization(now time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(now)
	currentUtilization := ct.cumulativeMeter.Read(now)
	ct.metrics.cumulativeMetric.Set(currentUtilization)
	return currentUtilization
}

func (ct *cpuTracker) Len() int {
	ct.lock.RLock()
	defer ct.lock.RUnlock()

	return ct.cpuSpenders.Len()
}

func (ct *cpuTracker) TimeUntilUtilization(
	nodeID ids.NodeID,
	now time.Time,
	value float64,
) time.Duration {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(now)

	meter, exists := ct.cpuSpenders.Get(nodeID)
	if !exists {
		return 0
	}
	return meter.(uptime.Meter).TimeUntil(now, value)
}

// prune attempts to remove cpu meters that currently show a value less than
// [epsilon].
//
// Because [cpuSpenders] isn't guaranteed to be sorted by their values, this
// doesn't guarantee that all meters showing less than [epsilon] are removed.
func (ct *cpuTracker) prune(now time.Time) {
	for {
		oldest, meterIntf, exists := ct.cpuSpenders.Oldest()
		if !exists {
			return
		}
		meter := meterIntf.(uptime.Meter)
		if meter.Read(now) > epsilon {
			return
		}
		ct.cpuSpenders.Delete(oldest)
		validatorID := oldest.(ids.NodeID)
		if weight, ok := ct.weights[validatorID]; ok {
			ct.activeWeight -= weight
			ct.metrics.activeWeightMetric.Set(float64(ct.activeWeight))
		}
		ct.metrics.activeLenMetric.Set(float64(ct.cpuSpenders.Len()))
	}
}

func (ct *cpuTracker) ActiveWeight() uint64 {
	ct.lock.RLock()
	defer ct.lock.RUnlock()

	return ct.activeWeight
}

type trackerMetrics struct {
	activeWeightMetric prometheus.Gauge
	activeLenMetric    prometheus.Gauge
	cumulativeMetric   prometheus.Gauge
}

func newCPUTrackerMetrics(namespace string, reg prometheus.Registerer) (*trackerMetrics, error) {
	m := &trackerMetrics{
		activeWeightMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "weight_active_nodes",
			Help:      "the sum of weight for all nodes considered active by the cpu tracker",
		}),
		activeLenMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_nodes",
			Help:      "the count of all nodes considered active by the cpu tracker",
		}),
		cumulativeMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cumulative_utilization",
			Help:      "an estimation of CPU utilization over all nodes considered active by the cpu tracker. range roughly [0, number of CPU cores], but can go higher due to over estimation",
		}),
	}
	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.activeWeightMetric),
		reg.Register(m.activeLenMetric),
		reg.Register(m.cumulativeMetric),
	)
	return m, errs.Err
}
