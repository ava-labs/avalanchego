// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade"
)

func GetConfig(fork Fork) upgrade.Config {
	c := upgrade.Config{
		ApricotPhase1Time:            upgrade.UnscheduledActivationTime,
		ApricotPhase2Time:            upgrade.UnscheduledActivationTime,
		ApricotPhase3Time:            upgrade.UnscheduledActivationTime,
		ApricotPhase4Time:            upgrade.UnscheduledActivationTime,
		ApricotPhase4MinPChainHeight: 0,
		ApricotPhase5Time:            upgrade.UnscheduledActivationTime,
		ApricotPhasePre6Time:         upgrade.UnscheduledActivationTime,
		ApricotPhase6Time:            upgrade.UnscheduledActivationTime,
		ApricotPhasePost6Time:        upgrade.UnscheduledActivationTime,
		BanffTime:                    upgrade.UnscheduledActivationTime,
		CortinaTime:                  upgrade.UnscheduledActivationTime,
		CortinaXChainStopVertexID:    ids.Empty,
		DurangoTime:                  upgrade.UnscheduledActivationTime,
		EtnaTime:                     upgrade.UnscheduledActivationTime,
	}

	switch fork {
	case Etna:
		c.EtnaTime = upgrade.InitiallyActiveTime
		fallthrough
	case Durango:
		c.DurangoTime = upgrade.InitiallyActiveTime
		fallthrough
	case Cortina:
		c.CortinaTime = upgrade.InitiallyActiveTime
		fallthrough
	case Banff:
		c.BanffTime = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhasePost6:
		c.ApricotPhasePost6Time = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhase6:
		c.ApricotPhase6Time = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhasePre6:
		c.ApricotPhasePre6Time = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhase5:
		c.ApricotPhase5Time = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhase4:
		c.ApricotPhase4Time = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhase3:
		c.ApricotPhase3Time = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhase2:
		c.ApricotPhase2Time = upgrade.InitiallyActiveTime
		fallthrough
	case ApricotPhase1:
		c.ApricotPhase1Time = upgrade.InitiallyActiveTime
	}
	return c
}
