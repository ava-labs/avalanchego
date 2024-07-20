// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	DefaultUpgradeTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	ApricotPhase1Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.March, 31, 14, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.March, 26, 14, 0, 0, 0, time.UTC),
	}

	ApricotPhase2Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.May, 10, 11, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.May, 5, 14, 0, 0, 0, time.UTC),
	}

	ApricotPhase3Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.August, 24, 14, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.August, 16, 19, 0, 0, 0, time.UTC),
	}

	ApricotPhase4Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.September, 22, 21, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.September, 16, 21, 0, 0, 0, time.UTC),
	}
	ApricotPhase4MinPChainHeight = map[uint32]uint64{
		constants.MainnetID: 793005,
		constants.FujiID:    47437,
	}

	ApricotPhase5Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.December, 2, 18, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.November, 24, 15, 0, 0, 0, time.UTC),
	}

	ApricotPhasePre6Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.September, 5, 1, 30, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC),
	}

	ApricotPhase6Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC),
	}

	ApricotPhasePost6Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.September, 7, 3, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.September, 7, 6, 0, 0, 0, time.UTC),
	}

	BanffTimes = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.October, 18, 16, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.October, 3, 14, 0, 0, 0, time.UTC),
	}

	CortinaTimes = map[uint32]time.Time{
		constants.MainnetID: time.Date(2023, time.April, 25, 15, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2023, time.April, 6, 15, 0, 0, 0, time.UTC),
	}
	CortinaXChainStopVertexID = map[uint32]ids.ID{
		// The mainnet stop vertex is well known. It can be verified on any
		// fully synced node by looking at the parentID of the genesis block.
		//
		// Ref: https://subnets.avax.network/x-chain/block/0
		constants.MainnetID: ids.FromStringOrPanic("jrGWDh5Po9FMj54depyunNixpia5PN4aAYxfmNzU8n752Rjga"),
		// The fuji stop vertex is well known. It can be verified on any fully
		// synced node by looking at the parentID of the genesis block.
		//
		// Ref: https://subnets-test.avax.network/x-chain/block/0
		constants.FujiID: ids.FromStringOrPanic("2D1cmbiG36BqQMRyHt4kFhWarmatA1ighSpND3FeFgz3vFVtCZ"),
	}

	DurangoTimes = map[uint32]time.Time{
		constants.MainnetID: time.Date(2024, time.March, 6, 16, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2024, time.February, 13, 16, 0, 0, 0, time.UTC),
	}

	EUpgradeTimes = map[uint32]time.Time{
		constants.MainnetID: time.Date(10000, time.December, 1, 0, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(10000, time.December, 1, 0, 0, 0, 0, time.UTC),
	}
)

var (
	Mainnet = Config{
		ApricotPhase1Time:            ApricotPhase1Times[constants.MainnetID],
		ApricotPhase2Time:            ApricotPhase2Times[constants.MainnetID],
		ApricotPhase3Time:            ApricotPhase3Times[constants.MainnetID],
		ApricotPhase4Time:            ApricotPhase4Times[constants.MainnetID],
		ApricotPhase4MinPChainHeight: ApricotPhase4MinPChainHeight[constants.MainnetID],
		ApricotPhase5Time:            ApricotPhase5Times[constants.MainnetID],
		ApricotPhasePre6Time:         ApricotPhasePre6Times[constants.MainnetID],
		ApricotPhase6Time:            ApricotPhase6Times[constants.MainnetID],
		ApricotPhasePost6Time:        ApricotPhasePost6Times[constants.MainnetID],
		BanffTime:                    BanffTimes[constants.MainnetID],
		CortinaTime:                  CortinaTimes[constants.MainnetID],
		CortinaXChainStopVertexID:    CortinaXChainStopVertexID[constants.MainnetID],
		DurangoTime:                  DurangoTimes[constants.MainnetID],
		EtnaUpgradeTime:              EUpgradeTimes[constants.MainnetID],
	}
	Fuji = Config{
		ApricotPhase1Time:            ApricotPhase1Times[constants.FujiID],
		ApricotPhase2Time:            ApricotPhase2Times[constants.FujiID],
		ApricotPhase3Time:            ApricotPhase3Times[constants.FujiID],
		ApricotPhase4Time:            ApricotPhase4Times[constants.FujiID],
		ApricotPhase4MinPChainHeight: ApricotPhase4MinPChainHeight[constants.FujiID],
		ApricotPhase5Time:            ApricotPhase5Times[constants.FujiID],
		ApricotPhasePre6Time:         ApricotPhasePre6Times[constants.FujiID],
		ApricotPhase6Time:            ApricotPhase6Times[constants.FujiID],
		ApricotPhasePost6Time:        ApricotPhasePost6Times[constants.FujiID],
		BanffTime:                    BanffTimes[constants.FujiID],
		CortinaTime:                  CortinaTimes[constants.FujiID],
		CortinaXChainStopVertexID:    CortinaXChainStopVertexID[constants.FujiID],
		DurangoTime:                  DurangoTimes[constants.FujiID],
		EtnaUpgradeTime:              EUpgradeTimes[constants.FujiID],
	}
	Default = Config{
		ApricotPhase1Time:            DefaultUpgradeTime,
		ApricotPhase2Time:            DefaultUpgradeTime,
		ApricotPhase3Time:            DefaultUpgradeTime,
		ApricotPhase4Time:            DefaultUpgradeTime,
		ApricotPhase4MinPChainHeight: 0,
		ApricotPhase5Time:            DefaultUpgradeTime,
		ApricotPhasePre6Time:         DefaultUpgradeTime,
		ApricotPhase6Time:            DefaultUpgradeTime,
		ApricotPhasePost6Time:        DefaultUpgradeTime,
		BanffTime:                    DefaultUpgradeTime,
		CortinaTime:                  DefaultUpgradeTime,
		CortinaXChainStopVertexID:    ids.Empty,
		DurangoTime:                  DefaultUpgradeTime,
		EtnaUpgradeTime:              DefaultUpgradeTime,
	}
)

type Config struct {
	ApricotPhase1Time            time.Time
	ApricotPhase2Time            time.Time
	ApricotPhase3Time            time.Time
	ApricotPhase4Time            time.Time
	ApricotPhase4MinPChainHeight uint64
	ApricotPhase5Time            time.Time
	ApricotPhasePre6Time         time.Time
	ApricotPhase6Time            time.Time
	ApricotPhasePost6Time        time.Time
	BanffTime                    time.Time
	CortinaTime                  time.Time
	CortinaXChainStopVertexID    ids.ID
	DurangoTime                  time.Time
	EtnaUpgradeTime              time.Time
}

func GetConfig(networkID uint32) Config {
	switch networkID {
	case constants.MainnetID:
		return Mainnet
	case constants.FujiID:
		return Fuji
	default:
		return Default
	}
}
