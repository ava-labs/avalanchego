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

const (
	epsilon = 1e-9
)

// TimeTracker is an interface for tracking peers' usage of CPU Time
type TimeTracker interface {
	UtilizeTime(ids.ShortID, time.Time, time.Time)
	Utilization(ids.ShortID, time.Time) float64
	CumulativeUtilization(time.Time) float64
	Len() int
}

// cpuTracker implements TimeTracker
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

func NewCPUTracker(factory uptime.Factory, halflife time.Duration) TimeTracker {
	return &cpuTracker{
		factory:         factory,
		cumulativeMeter: factory.New(halflife),
		halflife:        halflife,
		cpuSpenders:     linkedhashmap.New(),
	}
}

// UtilizeTime registers the use of CPU time by [vdr] from [startTime] to
// [endTime]
func (ct *cpuTracker) UtilizeTime(vdr ids.ShortID, startTime, endTime time.Time) {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	var meter uptime.Meter
	if meterIntf, exists := ct.cpuSpenders.Get(vdr); exists {
		meter = meterIntf.(uptime.Meter)
	} else {
		meter = ct.factory.New(ct.halflife)
	}
	// Put is called when the map already contains the meter in order to move
	// the meter to the back of the list.
	ct.cpuSpenders.Put(vdr, meter)

	ct.cumulativeMeter.Start(startTime)
	ct.cumulativeMeter.Stop(endTime)
	meter.Start(startTime)
	meter.Stop(endTime)

	ct.prune(endTime)
}

// Utilization returns the current EWMA of CPU utilization for [vdr]
func (ct *cpuTracker) Utilization(vdr ids.ShortID, currentTime time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(currentTime)

	meter, exists := ct.cpuSpenders.Get(vdr)
	if !exists {
		return 0
	}
	return meter.(uptime.Meter).Read(currentTime)
}

// CumulativeUtilization returns the cumulative EWMA of CPU utilization
func (ct *cpuTracker) CumulativeUtilization(currentTime time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	ct.prune(currentTime)

	return ct.cumulativeMeter.Read(currentTime)
}

// Len returns the number of CPU spenders that have recently spent CPU time
func (ct *cpuTracker) Len() int {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	return ct.cpuSpenders.Len()
}

// prune attempts to remove cpu meters that currently show a value less than
// [epsilon].
//
// Because [cpuSpenders] isn't guaranteed to be sorted by their values, this
// doesn't guarantee that all meters showing less than [epsilon] are removed.
func (ct *cpuTracker) prune(currentTime time.Time) {
	for {
		oldest, meterIntf, exists := ct.cpuSpenders.Oldest()
		if !exists {
			return
		}
		meter := meterIntf.(uptime.Meter)
		if meter.Read(currentTime) > epsilon {
			return
		}
		ct.cpuSpenders.Delete(oldest)
	}
}
