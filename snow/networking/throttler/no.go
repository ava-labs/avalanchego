// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"time"

	"github.com/ava-labs/avalanche-go/ids"
)

type noCountThrottler struct{}

func (noCountThrottler) Add(ids.ShortID) {}

func (noCountThrottler) Remove(ids.ShortID) {}

func (noCountThrottler) Throttle(ids.ShortID) bool { return false }

func (noCountThrottler) EndInterval() {}

// NewNoCountThrottler returns a CountingThrottler that will never throttle
func NewNoCountThrottler() CountingThrottler { return noCountThrottler{} }

type noCPUTracker struct{}

func (noCPUTracker) UtilizeCPU(ids.ShortID, time.Duration) {}

func (noCPUTracker) GetUtilization(ids.ShortID) float64 { return 0 }

func (noCPUTracker) EndInterval() {}

// NewNoCPUTracker returns a CPUTracker that does not track CPU usage and
// always returns 0 for the utilization value
func NewNoCPUTracker() CPUTracker { return noCPUTracker{} }
