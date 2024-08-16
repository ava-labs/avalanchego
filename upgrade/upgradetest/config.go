// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade"
)

func GetConfig(fork Fork) upgrade.Config {
	return GetConfigWithUpgradeTime(fork, upgrade.InitiallyActiveTime)
}

func GetConfigWithUpgradeTime(fork Fork, upgradeTime time.Time) upgrade.Config {
	c := upgrade.Config{
		ApricotPhase4MinPChainHeight: 0,
		CortinaXChainStopVertexID:    ids.Empty,
	}
	SetConfigTimesTo(&c, Latest, upgrade.UnscheduledActivationTime)
	SetConfigTimesTo(&c, fork, upgradeTime)
	return c
}

func SetConfigTimesTo(c *upgrade.Config, fork Fork, upgradeTime time.Time) {
	switch fork {
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
