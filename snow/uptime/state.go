// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type State interface {
	GetUptime(nodeID ids.NodeID) (upDuration time.Duration, lastUpdated time.Time, err error)
	SetUptime(nodeID ids.NodeID, upDuration time.Duration, lastUpdated time.Time) error
	GetStartTime(nodeID ids.NodeID) (startTime time.Time, err error)
}
