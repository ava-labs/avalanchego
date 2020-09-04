// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/uptime"
)

// TimeTracker is an interface for tracking peers' usage of CPU Time
type TimeTracker interface {
	GetMeter(ids.ShortID) uptime.Meter
	UtilizeTime(ids.ShortID, time.Time, time.Time)
	Utilization(ids.ShortID, time.Time) float64
	CumulativeUtilization(time.Time) float64
	Len() int
	EndInterval(time.Time)
}

type cpuTracker struct {
	lock sync.Mutex

	cumulativeMeter uptime.Meter
	halflife        time.Duration
	cpuSpenders     map[[20]byte]uptime.Meter
}

// NewCPUTracker ...
func NewCPUTracker(halflife time.Duration) TimeTracker {
	return &cpuTracker{
		halflife:        halflife,
		cumulativeMeter: uptime.NewIntervalMeter(halflife),
		cpuSpenders:     make(map[[20]byte]uptime.Meter),
	}
}

// GetMeter returns the meter used to measure CPU time spent processing
// messages from [vdr]
func (ct *cpuTracker) GetMeter(vdr ids.ShortID) uptime.Meter {
	key := vdr.Key()
	meter, exists := ct.cpuSpenders[key]
	if exists {
		return meter
	}

	newMeter := uptime.NewIntervalMeter(ct.halflife)
	ct.cpuSpenders[key] = newMeter
	return newMeter
}

// UtilizeTime registers the use of CPU time by [vdr] from [startTime]
// to [endTime]
func (ct *cpuTracker) UtilizeTime(vdr ids.ShortID, startTime, endTime time.Time) {
	meter := ct.GetMeter(vdr)
	ct.cumulativeMeter.Start(startTime)
	ct.cumulativeMeter.Stop(endTime)
	meter.Start(startTime)
	meter.Stop(endTime)
}

// Utilization returns the current EWMA of CPU utilization for [vdr]
func (ct *cpuTracker) Utilization(vdr ids.ShortID, currentTime time.Time) float64 {
	meter := ct.GetMeter(vdr)
	return meter.Read(currentTime)
}

// CumulativeUtilization returns the cumulative EWMA of CPU utilization
func (ct *cpuTracker) CumulativeUtilization(currentTime time.Time) float64 {
	return ct.cumulativeMeter.Read(currentTime)
}

// Len returns the number of CPU spenders that have recently
// spent CPU time
func (ct *cpuTracker) Len() int {
	return len(ct.cpuSpenders)
}

// EndInterval registers the end of a halflife interval for the CPU
// tracker
func (ct *cpuTracker) EndInterval(currentTime time.Time) {
	for key, meter := range ct.cpuSpenders {
		if meter.Read(currentTime) == 0 {
			delete(ct.cpuSpenders, key)
		}
	}
}
