// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var NoOpCalculator Calculator = TestCalculator{}

type TestCalculator struct {
	Percent float64
	Err     error
}

func (c TestCalculator) CalculateUptime(ids.NodeID) (time.Duration, time.Time, error) {
	return 0, time.Time{}, c.Err
}

func (c TestCalculator) CalculateUptimePercent(ids.NodeID) (float64, error) {
	return c.Percent, c.Err
}

func (c TestCalculator) CalculateUptimePercentFrom(ids.NodeID, time.Time) (float64, error) {
	return c.Percent, c.Err
}
