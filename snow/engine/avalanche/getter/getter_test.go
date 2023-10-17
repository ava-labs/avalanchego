// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var errUnknownVertex = errors.New("unknown vertex")

func testSetup(t *testing.T) (*vertex.TestManager, *common.SenderTest, common.Config) {
	vdrs := validators.NewManager()
	peer := ids.GenerateTestNodeID()
	require.NoError(t, vdrs.AddStaker(constants.PrimaryNetworkID, peer, nil, ids.Empty, 1))

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	isBootstrapped := false
	bootstrapTracker := &common.BootstrapTrackerTest{
		T: t,
		IsBootstrappedF: func() bool {
			return isBootstrapped
		},
		BootstrappedF: func(ids.ID) {
			isBootstrapped = true
		},
	}

	totalWeight, err := vdrs.TotalWeight(constants.PrimaryNetworkID)
	require.NoError(t, err)

	commonConfig := common.Config{
		Ctx:                            snow.DefaultConsensusContextTest(),
		Beacons:                        vdrs,
		SampleK:                        vdrs.Count(constants.PrimaryNetworkID),
		Alpha:                          totalWeight/2 + 1,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)

	return manager, sender, commonConfig
}

func TestAcceptedFrontier(t *testing.T) {
	require := require.New(t)

	manager, sender, config := testSetup(t)

	vtxID := ids.GenerateTestID()

	bsIntf, err := New(manager, config)
	require.NoError(err)
	require.IsType(&getter{}, bsIntf)
	bs := bsIntf.(*getter)

	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{
			vtxID,
		}
	}

	var accepted ids.ID
	sender.SendAcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, containerID ids.ID) {
		accepted = containerID
	}
	require.NoError(bs.GetAcceptedFrontier(context.Background(), ids.EmptyNodeID, 0))
	require.Equal(vtxID, accepted)
}

func TestFilterAccepted(t *testing.T) {
	require := require.New(t)

	manager, sender, config := testSetup(t)

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()
	vtxID2 := ids.GenerateTestID()

	vtx0 := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     vtxID0,
		StatusV: choices.Accepted,
	}}
	vtx1 := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     vtxID1,
		StatusV: choices.Accepted,
	}}

	bsIntf, err := New(manager, config)
	require.NoError(err)
	require.IsType(&getter{}, bsIntf)
	bs := bsIntf.(*getter)

	vtxIDs := []ids.ID{vtxID0, vtxID1, vtxID2}

	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID0:
			return vtx0, nil
		case vtxID1:
			return vtx1, nil
		case vtxID2:
			return nil, errUnknownVertex
		}
		require.FailNow(errUnknownVertex.Error())
		return nil, errUnknownVertex
	}

	var accepted []ids.ID
	sender.SendAcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	require.NoError(bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, vtxIDs))

	require.Contains(accepted, vtxID0)
	require.Contains(accepted, vtxID1)
	require.NotContains(accepted, vtxID2)
}
