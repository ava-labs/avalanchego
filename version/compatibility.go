// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// Compatibility is a utility for checking whether a peers version is
// compatible to the local version.
type Compatibility struct {
	// Ordering expectations are:
	// Current >= minCompatibleAfterUpgrade >= minCompatible
	Current                   *Application
	MinCompatibleAfterUpgrade *Application
	MinCompatible             *Application

	UpgradeTime time.Time
	clock       mockable.Clock
}

// Compatible returns whether if the provided version is compatible with the
// locally running version.
//
// This means that the version is connectable and that consensus messages can be
// made to the peer.
func (c *Compatibility) Compatible(peer *Application) bool {
	if c.Current.Major < peer.Major {
		return false // If we are on an older major version, we are incompatible.
	}

	minCompatibleVersion := c.MinCompatibleAfterUpgrade
	if now := c.clock.Time(); now.Before(c.UpgradeTime) {
		minCompatibleVersion = c.MinCompatible
	}
	return peer.Compare(minCompatibleVersion) >= 0 // Peer must be at least the min compatible version
}
