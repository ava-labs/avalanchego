// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import (
	"time"

	"github.com/ava-labs/avalanchego/upgrade"
)

// GetConfig returns an upgrade config with the provided fork scheduled to have
// been initially activated and all other forks to be unscheduled.
func GetConfig(fork Fork) upgrade.Config {
	return GetConfigWithUpgradeTime(fork, upgrade.InitiallyActiveTime)
}

// GetConfigWithUpgradeTime returns an upgrade config with the provided fork
// scheduled to be activated at the provided upgradeTime and all other forks to
// be unscheduled.
func GetConfigWithUpgradeTime(fork Fork, upgradeTime time.Time) upgrade.Config {
	c := upgrade.Config{}
	// Initialize all forks to be unscheduled
	SetTimesTo(&c, Latest, upgrade.UnscheduledActivationTime)
	// Schedule the requested forks at the provided upgrade time
	SetTimesTo(&c, fork, upgradeTime)
	return c
}

// SetTimesTo sets the upgrade time of the provided fork, and all prior forks,
// to the provided upgradeTime.
func SetTimesTo(c *upgrade.Config, fork Fork, upgradeTime time.Time) {
	switch fork {
	case Granite:
		c.GraniteTime = upgradeTime
		fallthrough
	case Fortuna:
		c.FortunaTime = upgradeTime
		fallthrough
	case Etna:
		c.EtnaTime = upgradeTime
		fallthrough
	case Durango:
		c.DurangoTime = upgradeTime
		fallthrough
	case Cortina:
		c.CortinaTime = upgradeTime
		fallthrough
	case Banff:
		c.BanffTime = upgradeTime
		fallthrough
	case ApricotPhasePost6:
		c.ApricotPhasePost6Time = upgradeTime
		fallthrough
	case ApricotPhase6:
		c.ApricotPhase6Time = upgradeTime
		fallthrough
	case ApricotPhasePre6:
		c.ApricotPhasePre6Time = upgradeTime
		fallthrough
	case ApricotPhase5:
		c.ApricotPhase5Time = upgradeTime
		fallthrough
	case ApricotPhase4:
		c.ApricotPhase4Time = upgradeTime
		fallthrough
	case ApricotPhase3:
		c.ApricotPhase3Time = upgradeTime
		fallthrough
	case ApricotPhase2:
		c.ApricotPhase2Time = upgradeTime
		fallthrough
	case ApricotPhase1:
		c.ApricotPhase1Time = upgradeTime
	}
}
