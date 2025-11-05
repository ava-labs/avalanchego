// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	// version >= minCompatibleAfterUpgrade >= minCompatible
	version                   *Application
	minCompatibleAfterUpgrade *Application
	minCompatible             *Application

	upgradeTime time.Time
	clock       mockable.Clock
}

// NewCompatibility returns a compatibility checker with the provided options.
func NewCompatibility(
	version *Application,
	minCompatibleAfterUpgrade *Application,
	minCompatible *Application,
	upgradeTime time.Time,
) *Compatibility {
	return &Compatibility{
		version:                   version,
		minCompatibleAfterUpgrade: minCompatibleAfterUpgrade,
		minCompatible:             minCompatible,
		upgradeTime:               upgradeTime,
	}
}

// Version returns the locally running version
func (c *Compatibility) Version() *Application {
	return c.version
}

// Compatible returns whether if the provided version is compatible with the
// locally running version.
//
// This means that the version is connectable and that consensus messages can be
// made to the peer.
func (c *Compatibility) Compatible(peer *Application) bool {
	if c.version.Major < peer.Major {
		return false // If we are on an older major version, we are incompatible.
	}

	minCompatibleVersion := c.minCompatibleAfterUpgrade
	if now := c.clock.Time(); now.Before(c.upgradeTime) {
		minCompatibleVersion = c.minCompatible
	}
	return peer.Compare(minCompatibleVersion) >= 0 // Peer must be at least the min compatible version
}
