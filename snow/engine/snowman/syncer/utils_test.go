// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	key         uint64 = 2022
	minorityKey uint64 = 2000
)

var (
	_ block.ChainVM         = fullVM{}
	_ block.StateSyncableVM = fullVM{}

	unknownSummaryID = ids.ID{'g', 'a', 'r', 'b', 'a', 'g', 'e'}

	summaryBytes = []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	summaryID    ids.ID

	minoritySummaryBytes = []byte{'m', 'i', 'n', 'o', 'r', 'i', 't', 'y'}
	minoritySummaryID    ids.ID
)

func init() {
	var err error
	summaryID, err = ids.ToID(hashing.ComputeHash256(summaryBytes))
	if err != nil {
		panic(err)
	}

	minoritySummaryID, err = ids.ToID(hashing.ComputeHash256(minoritySummaryBytes))
	if err != nil {
		panic(err)
	}
}

type fullVM struct {
	*blocktest.VM
	*blocktest.StateSyncableVM
}

func buildTestPeers(t *testing.T, subnetID ids.ID) validators.Manager {
	// We consider more than maxOutstandingBroadcastRequests peers to test
	// capping the number of requests sent out.
	vdrs := validators.NewManager()
	for idx := 0; idx < 2*maxOutstandingBroadcastRequests; idx++ {
		beaconID := ids.GenerateTestNodeID()
		require.NoError(t, vdrs.AddStaker(subnetID, beaconID, nil, ids.Empty, 1))
	}
	return vdrs
}

func buildTestsObjects(
	t *testing.T,
	ctx *snow.ConsensusContext,
	startupTracker tracker.Startup,
	beacons validators.Manager,
	alpha uint64,
) (
	*stateSyncer,
	*fullVM,
	*enginetest.Sender,
) {
	require := require.New(t)

	fullVM := &fullVM{
		VM: &blocktest.VM{
			VM: enginetest.VM{T: t},
		},
		StateSyncableVM: &blocktest.StateSyncableVM{
			T: t,
		},
	}
	sender := &enginetest.Sender{T: t}
	dummyGetter, err := getter.New(
		fullVM,
		sender,
		ctx.Log,
		time.Second,
		2000,
		ctx.Registerer,
	)
	require.NoError(err)

	cfg, err := NewConfig(
		dummyGetter,
		ctx,
		startupTracker,
		sender,
		beacons,
		beacons.NumValidators(ctx.SubnetID),
		alpha,
		nil,
		fullVM,
	)
	require.NoError(err)
	commonSyncer := New(cfg, func(context.Context, uint32) error {
		return nil
	})
	require.IsType(&stateSyncer{}, commonSyncer)
	syncer := commonSyncer.(*stateSyncer)
	require.NotNil(syncer.stateSyncVM)

	fullVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}

	return syncer, fullVM, sender
}

func pickRandomFrom(nodes map[ids.NodeID]uint32) ids.NodeID {
	for node := range nodes {
		return node
	}
	return ids.EmptyNodeID
}
