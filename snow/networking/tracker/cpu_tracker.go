// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/uptime"
)

const epsilon = 1e-9

var _ TimeTracker = &cpuTracker{}

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
}

type cpuTracker struct {
	lock sync.Mutex

	factory         uptime.Factory
	cumulativeMeter uptime.Meter
	halflife        time.Duration
	// cpuSpenders is ordered by the last time that a meter was utilized. This
	// doesn't necessarily result in the meters being sorted based on their
	// current utilization. However, in practice the nodes that are not being
	// utilized will move towards the oldest elements where they can be deleted.
	cpuSpenders linkedhashmap.LinkedHashmap
}

// NewCPUTracker returns a new TimeTracker that is safe for concurrent access by multiple goroutines.
func NewCPUTracker(factory uptime.Factory, halflife time.Duration) TimeTracker {
	return &cpuTracker{
		factory:         factory,
		cumulativeMeter: factory.New(halflife),
		halflife:        halflife,
		cpuSpenders:     linkedhashmap.New(),
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

	return ct.cumulativeMeter.Read(now)
}

func (ct *cpuTracker) Len() int {
	ct.lock.Lock()
	defer ct.lock.Unlock()

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
	}
}
