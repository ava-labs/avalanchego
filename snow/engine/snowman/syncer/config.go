// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	// SampleK determines the number of nodes to attempt to fetch the latest
	// state sync summary from. In order for a round of voting to succeed, there
	// must be at least one correct node sampled.
	SampleK int

	// Alpha specifies the amount of weight that validators must put behind a
	// state summary to consider it valid to sync to.
	Alpha uint64

	// StateSyncBeacons are the nodes that will be used to sample and vote over
	// state summaries.
	StateSyncBeacons validators.Set

	VM block.ChainVM
}

func NewConfig(
	commonCfg common.Config,
	stateSyncerIDs []ids.NodeID,
	snowGetHandler common.AllGetsServer,
	vm block.ChainVM,
) (Config, error) {
	// Initialize the default values that will be used if stateSyncerIDs is
	// empty.
	var (
		stateSyncBeacons = commonCfg.Beacons
		syncAlpha        = commonCfg.Alpha
		syncSampleK      = commonCfg.SampleK
	)

	// If the user has manually provided state syncer IDs, then override the
	// state sync beacons to them.
	if len(stateSyncerIDs) != 0 {
		stateSyncBeacons = validators.NewSet()
		for _, peerID := range stateSyncerIDs {
			// Invariant: We never use the TxID or BLS keys populated here.
			if err := stateSyncBeacons.Add(peerID, nil, ids.Empty, 1); err != nil {
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
