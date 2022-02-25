// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type Config struct {
	SampleK          int
	Alpha            uint64
	StateSyncBeacons validators.Set

	common.AllGetsServer
	Sender        common.Sender
	Ctx           *snow.ConsensusContext
	VM            block.ChainVM
	WeightTracker tracker.WeightTracker

	RetrySyncing              bool
	RetrySyncingWarnFrequency int
}

func NewConfig(
	commonCfg common.Config,
	stateSyncerIDs []ids.ShortID,
	snowGetHandler common.AllGetsServer,
	vm block.ChainVM,
	weightTracker tracker.WeightTracker,
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
		SampleK:                   syncSampleK,
		Alpha:                     syncAlpha,
		StateSyncBeacons:          stateSyncBeacons,
		Sender:                    commonCfg.Sender,
		AllGetsServer:             snowGetHandler,
		Ctx:                       commonCfg.Ctx,
		VM:                        vm,
		WeightTracker:             weightTracker,
		RetrySyncing:              commonCfg.RetryBootstrap,
		RetrySyncingWarnFrequency: commonCfg.RetryBootstrapWarnFrequency,
	}, nil
}
