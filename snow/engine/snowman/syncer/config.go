// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type Config struct {
	common.AllGetsServer

	Ctx *snow.ConsensusContext

	StartupTracker tracker.Startup
	Sender         common.Sender

	// SampleK determines the number of nodes to attempt to fetch the latest
	// state sync summary from. In order for a round of voting to succeed, there
	// must be at least one correct node sampled.
	SampleK int

	// Alpha specifies the amount of weight that validators must put behind a
	// state summary to consider it valid to sync to.
	Alpha uint64

	// StateSyncBeacons are the nodes that will be used to sample and vote over
	// state summaries.
	StateSyncBeacons validators.Manager

	VM block.ChainVM
}

func NewConfig(
	snowGetHandler common.AllGetsServer,
	ctx *snow.ConsensusContext,
	startupTracker tracker.Startup,
	sender common.Sender,
	beacons validators.Manager,
	sampleK int,
	alpha uint64,
	stateSyncerIDs []ids.NodeID,
	vm block.ChainVM,
) (Config, error) {
	// Initialize the beacons that will be used if stateSyncerIDs is empty.
	stateSyncBeacons := beacons

	// If the user has manually provided state syncer IDs, then override the
	// state sync beacons to them.
	if len(stateSyncerIDs) != 0 {
		stateSyncBeacons = validators.NewManager()
		for _, peerID := range stateSyncerIDs {
			// Invariant: We never use the TxID or BLS keys populated here.
			if err := stateSyncBeacons.AddStaker(ctx.SubnetID, peerID, nil, ids.Empty, 1); err != nil {
				return Config{}, err
			}
		}
		stateSyncingWeight, err := stateSyncBeacons.TotalWeight(ctx.SubnetID)
		if err != nil {
			return Config{}, fmt.Errorf("failed to calculate total weight of state sync beacons for subnet %s: %w", ctx.SubnetID, err)
		}
		sampleK = int(min(uint64(sampleK), stateSyncingWeight))
		alpha = stateSyncingWeight/2 + 1 // must be > 50%
	}
	return Config{
		AllGetsServer:    snowGetHandler,
		Ctx:              ctx,
		StartupTracker:   startupTracker,
		Sender:           sender,
		SampleK:          sampleK,
		Alpha:            alpha,
		StateSyncBeacons: stateSyncBeacons,
		VM:               vm,
	}, nil
}
