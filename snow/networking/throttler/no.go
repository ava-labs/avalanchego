// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"time"

	"github.com/ava-labs/gecko/ids"
)

type noThrottler struct{}

func (noThrottler) AddMessage(ids.ShortID) {}

func (noThrottler) RemoveMessage(ids.ShortID) {}

func (noThrottler) UtilizeCPU(ids.ShortID, time.Duration) {}

func (noThrottler) GetUtilization(ids.ShortID) (float64, bool) { return 0, false }

func (noThrottler) EndInterval() {}

// NewNoThrottler returns a throttler that will never throttle
func NewNoThrottler() Throttler { return noThrottler{} }
