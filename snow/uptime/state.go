// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type State interface {
	// GetUptime returns [upDuration] and [lastUpdated] of [nodeID]
	// Returns [database.ErrNotFound] if [nodeID] isn't currently a validator.
	GetUptime(
		nodeID ids.NodeID,
	) (upDuration time.Duration, lastUpdated time.Time, err error)

	// SetUptime updates [upDuration] and [lastUpdated] of [nodeID]
	// Returns [database.ErrNotFound] if [nodeID] isn't currently a validator
	// Invariant: expects [lastUpdated] to be truncated (floored) to the nearest
	//            second.
	SetUptime(
		nodeID ids.NodeID,
		upDuration time.Duration,
		lastUpdated time.Time,
	) error

	// GetStartTime returns the time that [nodeID] started validating.
	// Returns [database.ErrNotFound] if [nodeID] isn't currently a validator.
	GetStartTime(
		nodeID ids.NodeID,
	) (startTime time.Time, err error)
}
