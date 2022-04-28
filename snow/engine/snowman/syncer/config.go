// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type Config struct {
	common.Config
	common.AllGetsServer

	SampleK          int
	Alpha            uint64
	StateSyncBeacons validators.Set

	VM block.ChainVM
}

func NewConfig(
	commonCfg common.Config,
	stateSyncerIDs []ids.NodeID,
	snowGetHandler common.AllGetsServer,
	vm block.ChainVM,
) (Config, error) {
	var (
		stateSyncBeacons = commonCfg.Beacons
		syncAlpha        = commonCfg.Alpha
		syncSampleK      = commonCfg.SampleK
	)

	if len(stateSyncerIDs) != 0 {
		stateSyncBeacons = validators.NewSet()
		for _, peerID := range stateSyncerIDs {
			if err := stateSyncBeacons.AddWeight(peerID, 1); err != nil {
				return Config{}, err
			}
		}
		stateSyncingWeight := stateSyncBeacons.Weight()
		if uint64(syncSampleK) > stateSyncingWeight {
			syncSampleK = int(stateSyncingWeight)
		}
		syncAlpha = stateSyncingWeight/2 + 1 // must be > 50%
	}
	return Config{
		Config:           commonCfg,
		AllGetsServer:    snowGetHandler,
		SampleK:          syncSampleK,
		Alpha:            syncAlpha,
		StateSyncBeacons: stateSyncBeacons,
		VM:               vm,
	}, nil
}
