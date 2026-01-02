// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var NoOpCalculator Calculator = noOpCalculator{}

type noOpCalculator struct{}

func (noOpCalculator) CalculateUptime(ids.NodeID) (time.Duration, time.Time, error) {
	return 0, time.Time{}, nil
}

func (noOpCalculator) CalculateUptimePercent(ids.NodeID) (float64, error) {
	return 0, nil
}

func (noOpCalculator) CalculateUptimePercentFrom(ids.NodeID, time.Time) (float64, error) {
	return 0, nil
}
