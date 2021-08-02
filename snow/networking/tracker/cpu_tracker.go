// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
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
	EndInterval(time.Time)
}

// cpuTracker implements TimeTracker
type cpuTracker struct {
	lock sync.Mutex

	factory         uptime.Factory
	cumulativeMeter uptime.Meter
	halflife        time.Duration
	cpuSpenders     map[ids.ShortID]uptime.Meter
}

func NewCPUTracker(factory uptime.Factory, halflife time.Duration) TimeTracker {
	return &cpuTracker{
		factory:         factory,
		cumulativeMeter: factory.New(halflife),
		halflife:        halflife,
		cpuSpenders:     make(map[ids.ShortID]uptime.Meter),
	}
}

// getMeter returns the meter used to measure CPU time spent processing
// messages from [vdr]
// assumes the lock is held
func (ct *cpuTracker) getMeter(vdr ids.ShortID) uptime.Meter {
	meter, exists := ct.cpuSpenders[vdr]
	if exists {
		return meter
	}

	newMeter := ct.factory.New(ct.halflife)
	ct.cpuSpenders[vdr] = newMeter
	return newMeter
}

// UtilizeTime registers the use of CPU time by [vdr] from [startTime]
// to [endTime]
func (ct *cpuTracker) UtilizeTime(vdr ids.ShortID, startTime, endTime time.Time) {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	meter := ct.getMeter(vdr)
	ct.cumulativeMeter.Start(startTime)
	ct.cumulativeMeter.Stop(endTime)
	meter.Start(startTime)
	meter.Stop(endTime)
}

// Utilization returns the current EWMA of CPU utilization for [vdr]
func (ct *cpuTracker) Utilization(vdr ids.ShortID, currentTime time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	meter := ct.getMeter(vdr)
	return meter.Read(currentTime)
}

// CumulativeUtilization returns the cumulative EWMA of CPU utilization
func (ct *cpuTracker) CumulativeUtilization(currentTime time.Time) float64 {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	return ct.cumulativeMeter.Read(currentTime)
}

// Len returns the number of CPU spenders that have recently
// spent CPU time
func (ct *cpuTracker) Len() int {
	ct.lock.Lock()
	defer ct.lock.Unlock()

	return len(ct.cpuSpenders)
}

// EndInterval registers the end of a halflife interval for the CPU
// tracker
func (ct *cpuTracker) EndInterval(currentTime time.Time) {
	ct.lock.Lock()
	defer ct.lock.Unlock()
	for key, meter := range ct.cpuSpenders {
		if meter.Read(currentTime) <= epsilon {
			delete(ct.cpuSpenders, key)
		}
	}
}
