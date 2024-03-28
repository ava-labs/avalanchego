// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func NextBlockTime(parentTime time.Time, clk *mockable.Clock) time.Time {
	// [timestamp] = max(now, parentTime)

	timestamp := clk.Time()
	if parentTime.After(timestamp) {
		timestamp = parentTime
	}
	return timestamp
}
