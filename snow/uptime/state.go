// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type State interface {
	// GetUptime returns [upDuration] and [lastUpdated] of [nodeID] on
	// [subnetID].
	// Returns [database.ErrNotFound] if [nodeID] isn't currently a validator of
	// the subnet.
	GetUptime(
		nodeID ids.NodeID,
		subnetID ids.ID,
	) (upDuration time.Duration, lastUpdated time.Time, err error)

	// SetUptime updates [upDuration] and [lastUpdated] of [nodeID] on
	// [subnetID].
	// Returns [database.ErrNotFound] if [nodeID] isn't currently a validator of
	// the subnet.
	// Invariant: expects [lastUpdated] to be truncated (floored) to the nearest
	//            second.
	SetUptime(
		nodeID ids.NodeID,
		subnetID ids.ID,
		upDuration time.Duration,
		lastUpdated time.Time,
	) error

	// GetStartTime returns the time that [nodeID] started validating
	// [subnetID].
	// Returns [database.ErrNotFound] if [nodeID] isn't currently a validator of
	// the subnet.
	GetStartTime(
		nodeID ids.NodeID,
		subnetID ids.ID,
	) (startTime time.Time, err error)
}
