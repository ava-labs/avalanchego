// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type State interface {
	// GetUptime returns [upDuration] and [lastUpdated] to be truncated (floored) to the nearest second
	GetUptime(nodeID ids.NodeID) (upDuration time.Duration, lastUpdated time.Time, err error)
	// SetUptime expects [upDuration] and [lastUpdated] to be truncated (floored) to the nearest second
	SetUptime(nodeID ids.NodeID, upDuration time.Duration, lastUpdated time.Time) error
	// GetStartTime returns [startTime] truncated (floored) to the nearest second
	GetStartTime(nodeID ids.NodeID) (startTime time.Time, err error)
}
