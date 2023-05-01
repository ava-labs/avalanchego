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
	"github.com/ava-labs/avalanchego/utils/set"
)

var errUnknownVertex = errors.New("unknown vertex")

func testSetup(t *testing.T) (*vertex.TestManager, *common.SenderTest, common.Config) {
	require := require.New(t)

	peers := validators.NewSet()
	peer := ids.GenerateTestNodeID()
	require.NoError(peers.Add(peer, nil, ids.Empty, 1))

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

	commonConfig := common.Config{
		Ctx:                            snow.DefaultConsensusContextTest(),
		Beacons:                        peers,
		SampleK:                        peers.Len(),
		Alpha:                          peers.Weight()/2 + 1,
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

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()
	vtxID2 := ids.GenerateTestID()

	bsIntf, err := New(manager, config)
	require.NoError(err)
	bs, ok := bsIntf.(*getter)
	if !ok {
		t.Fatal("Unexpected get handler")
	}

	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{
			vtxID0,
			vtxID1,
		}
	}

	var accepted []ids.ID
	sender.SendAcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	require.NoError(bs.GetAcceptedFrontier(context.Background(), ids.EmptyNodeID, 0))

	acceptedSet := set.Set[ids.ID]{}
	acceptedSet.Add(accepted...)

	manager.EdgeF = nil

	if !acceptedSet.Contains(vtxID0) {
		t.Fatalf("Vtx should be accepted")
	}
	if !acceptedSet.Contains(vtxID1) {
		t.Fatalf("Vtx should be accepted")
	}
	if acceptedSet.Contains(vtxID2) {
		t.Fatalf("Vtx shouldn't be accepted")
	}
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
	bs, ok := bsIntf.(*getter)
	if !ok {
		t.Fatal("Unexpected get handler")
	}

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
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	var accepted []ids.ID
	sender.SendAcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	require.NoError(bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, vtxIDs))

	acceptedSet := set.Set[ids.ID]{}
	acceptedSet.Add(accepted...)

	manager.GetVtxF = nil

	if !acceptedSet.Contains(vtxID0) {
		t.Fatalf("Vtx should be accepted")
	}
	if !acceptedSet.Contains(vtxID1) {
		t.Fatalf("Vtx should be accepted")
	}
	if acceptedSet.Contains(vtxID2) {
		t.Fatalf("Vtx shouldn't be accepted")
	}
}
