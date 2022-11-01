// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/version"
)

var errUnknownBlock = errors.New("unknown block")

func newConfig(t *testing.T) (Config, ids.NodeID, *common.SenderTest, *block.TestVM) {
	ctx := snow.DefaultConsensusContextTest()

	peers := validators.NewSet()

	sender := &common.SenderTest{}
	vm := &block.TestVM{}

	sender.T = t
	vm.T = t

	sender.Default(true)
	vm.Default(true)

	isBootstrapped := false
	subnet := &common.SubnetTest{
		T:               t,
		IsBootstrappedF: func() bool { return isBootstrapped },
		BootstrappedF:   func(ids.ID) { isBootstrapped = true },
	}

	sender.CantSendGetAcceptedFrontier = false

	peer := ids.GenerateTestNodeID()
	if err := peers.AddWeight(peer, 1); err != nil {
		t.Fatal(err)
	}

	peerTracker := tracker.NewPeers()
	startupTracker := tracker.NewStartup(peerTracker, peers.Weight()/2+1)
	peers.RegisterCallbackListener(startupTracker)

	if err := startupTracker.Connected(peer, version.CurrentApp); err != nil {
		t.Fatal(err)
	}

	commonConfig := common.Config{
		Ctx:                            ctx,
		Validators:                     peers,
		Beacons:                        peers,
		SampleK:                        peers.Len(),
		Alpha:                          peers.Weight()/2 + 1,
		StartupTracker:                 startupTracker,
		Sender:                         sender,
		Subnet:                         subnet,
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	snowGetHandler, err := getter.New(vm, commonConfig)
	if err != nil {
		t.Fatal(err)
	}

	blocker, _ := queue.NewWithMissing(memdb.New(), "", prometheus.NewRegistry())
	return Config{
		Config:        commonConfig,
		AllGetsServer: snowGetHandler,
		Blocked:       blocker,
		VM:            vm,
	}, peer, sender, vm
}

func TestBootstrapperStartsOnlyIfEnoughStakeIsConnected(t *testing.T) {
	require := require.New(t)

	sender := &common.SenderTest{T: t}
	vm := &block.TestVM{
		TestVM: common.TestVM{T: t},
	}

	sender.Default(true)
	vm.Default(true)

	// create boostrapper configuration
	peers := validators.NewSet()
	sampleK := 2
	alpha := uint64(10)
	startupAlpha := alpha

	peerTracker := tracker.NewPeers()
	startupTracker := tracker.NewStartup(peerTracker, startupAlpha)
	peers.RegisterCallbackListener(startupTracker)

	commonCfg := common.Config{
		Ctx:                            snow.DefaultConsensusContextTest(),
		Validators:                     peers,
		Beacons:                        peers,
		SampleK:                        sampleK,
		Alpha:                          alpha,
		StartupTracker:                 startupTracker,
		Sender:                         sender,
		Subnet:                         &common.SubnetTest{},
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	blocker, _ := queue.NewWithMissing(memdb.New(), "", prometheus.NewRegistry())
	snowGetHandler, err := getter.New(vm, commonCfg)
	require.NoError(err)
	cfg := Config{
		Config:        commonCfg,
		AllGetsServer: snowGetHandler,
		Blocked:       blocker,
		VM:            vm,
	}

	blkID0 := ids.Empty.Prefix(0)
	blkBytes0 := []byte{0}
	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		require.Equal(blk0.ID(), blkID)
		return blk0, nil
	}

	// create bootstrapper
	dummyCallback := func(lastReqID uint32) error { cfg.Ctx.SetState(snow.NormalOp); return nil }
	bs, err := New(cfg, dummyCallback)
	require.NoError(err)

	vm.CantSetState = false
	vm.CantConnected = true
	vm.ConnectedF = func(ids.NodeID, *version.Application) error { return nil }

	frontierRequested := false
	sender.CantSendGetAcceptedFrontier = false
	sender.SendGetAcceptedFrontierF = func(_ context.Context, ss ids.NodeIDSet, u uint32) {
		frontierRequested = true
	}

	// attempt starting bootstrapper with no stake connected. Bootstrapper should stall.
	require.NoError(bs.Start(0))
	require.False(frontierRequested)

	// attempt starting bootstrapper with not enough stake connected. Bootstrapper should stall.
	vdr0 := ids.GenerateTestNodeID()
	require.NoError(peers.AddWeight(vdr0, startupAlpha/2))
	require.NoError(bs.Connected(vdr0, version.CurrentApp))

	require.NoError(bs.Start(0))
	require.False(frontierRequested)

	// finally attempt starting bootstrapper with enough stake connected. Frontiers should be requested.
	vdr := ids.GenerateTestNodeID()
	require.NoError(peers.AddWeight(vdr, startupAlpha))
	require.NoError(bs.Connected(vdr, version.CurrentApp))
	require.True(frontierRequested)
}

// Single node in the accepted frontier; no need to fetch parent
func TestBootstrapperSingleFrontier(t *testing.T) {
	config, _, _, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Processing,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		require.Equal(t, blk0.ID(), blkID)
		return blk0, nil
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = false
	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{blkID1}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID1:
			return blk1, nil
		case blkID0:
			return blk0, nil
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes1):
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	err = bs.ForceAccepted(context.Background(), acceptedIDs)
	switch {
	case err != nil: // should finish
		t.Fatal(err)
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

// Requests the unknown block and gets back a Ancestors with unexpected request ID.
// Requests again and gets response from unexpected peer.
// Requests again and gets an unexpected block.
// Requests again and gets the expected block.
func TestBootstrapperUnknownByzantineResponse(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Processing,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}

	vm.CantSetState = false
	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		require.Equal(t, blk0.ID(), blkID)
		return blk0, nil
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{blkID2}

	parsedBlk1 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, database.ErrNotFound
		case blkID2:
			return blk2, nil
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			return blk2, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestID := new(uint32)
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID == blkID1:
		default:
			t.Fatalf("should have requested blk1")
		}
		*requestID = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(context.Background(), acceptedIDs); err != nil { // should request blk1
		t.Fatal(err)
	}

	oldReqID := *requestID
	if err := bs.Ancestors(context.Background(), peerID, *requestID+1, [][]byte{blkBytes1}); err != nil { // respond with wrong request ID
		t.Fatal(err)
	} else if oldReqID != *requestID {
		t.Fatal("should not have sent new request")
	}

	if err := bs.Ancestors(context.Background(), ids.NodeID{1, 2, 3}, *requestID, [][]byte{blkBytes1}); err != nil { // respond from wrong peer
		t.Fatal(err)
	} else if oldReqID != *requestID {
		t.Fatal("should not have sent new request")
	}

	if err := bs.Ancestors(context.Background(), peerID, *requestID, [][]byte{blkBytes0}); err != nil { // respond with wrong block
		t.Fatal(err)
	} else if oldReqID == *requestID {
		t.Fatal("should have sent new request")
	}

	err = bs.Ancestors(context.Background(), peerID, *requestID, [][]byte{blkBytes1})
	switch {
	case err != nil: // respond with right block
		t.Fatal(err)
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

// There are multiple needed blocks and Ancestors returns one at a time
func TestBootstrapperPartialFetch(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)
	blkID3 := ids.Empty.Prefix(3)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}
	blkBytes3 := []byte{3}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Processing,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		require.Equal(t, blk0.ID(), blkID)
		return blk0, nil
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = false
	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{blkID3}

	parsedBlk1 := false
	parsedBlk2 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, database.ErrNotFound
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, database.ErrNotFound
		case blkID3:
			return blk3, nil
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		case bytes.Equal(blkBytes, blkBytes3):
			return blk3, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestID := new(uint32)
	requested := ids.Empty
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch vtxID {
		case blkID1, blkID2:
		default:
			t.Fatalf("should have requested blk1 or blk2")
		}
		*requestID = reqID
		requested = vtxID
	}

	if err := bs.ForceAccepted(context.Background(), acceptedIDs); err != nil { // should request blk2
		t.Fatal(err)
	}

	if err := bs.Ancestors(context.Background(), peerID, *requestID, [][]byte{blkBytes2}); err != nil { // respond with blk2
		t.Fatal(err)
	} else if requested != blkID1 {
		t.Fatal("should have requested blk1")
	}

	if err := bs.Ancestors(context.Background(), peerID, *requestID, [][]byte{blkBytes1}); err != nil { // respond with blk1
		t.Fatal(err)
	} else if requested != blkID1 {
		t.Fatal("should not have requested another block")
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

// There are multiple needed blocks and some validators do not have all the blocks
// This test was modeled after TestBootstrapperPartialFetch.
func TestBootstrapperEmptyResponse(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)
	blkID3 := ids.Empty.Prefix(3)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}
	blkBytes3 := []byte{3}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Processing,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		require.Equal(t, blk0.ID(), blkID)
		return blk0, nil
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = false
	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{blkID3}

	parsedBlk1 := false
	parsedBlk2 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, database.ErrNotFound
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, database.ErrNotFound
		case blkID3:
			return blk3, nil
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		case bytes.Equal(blkBytes, blkBytes3):
			return blk3, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestedVdr := ids.EmptyNodeID
	requestID := uint32(0)
	requestedBlock := ids.Empty
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, blkID ids.ID) {
		requestedVdr = vdr
		requestID = reqID
		requestedBlock = blkID
	}

	// should request blk2
	err = bs.ForceAccepted(context.Background(), acceptedIDs)
	switch {
	case err != nil:
		t.Fatal(err)
	case requestedVdr != peerID:
		t.Fatal("should have requested from peerID")
	case requestedBlock != blkID2:
		t.Fatal("should have requested blk2")
	}

	// add another two validators to the fetch set to test behavior on empty response
	newPeerID := ids.GenerateTestNodeID()
	bs.(*bootstrapper).fetchFrom.Add(newPeerID)

	newPeerID = ids.GenerateTestNodeID()
	bs.(*bootstrapper).fetchFrom.Add(newPeerID)

	if err := bs.Ancestors(context.Background(), peerID, requestID, [][]byte{blkBytes2}); err != nil { // respond with blk2
		t.Fatal(err)
	} else if requestedBlock != blkID1 {
		t.Fatal("should have requested blk1")
	}

	peerToBlacklist := requestedVdr

	// respond with empty
	err = bs.Ancestors(context.Background(), peerToBlacklist, requestID, nil)
	switch {
	case err != nil:
		t.Fatal(err)
	case requestedVdr == peerToBlacklist:
		t.Fatal("shouldn't have requested from peerToBlacklist")
	case requestedBlock != blkID1:
		t.Fatal("should have requested blk1")
	}

	if err := bs.Ancestors(context.Background(), requestedVdr, requestID, [][]byte{blkBytes1}); err != nil { // respond with blk1
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}

	// check peerToBlacklist was removed from the fetch set
	require.False(t, bs.(*bootstrapper).fetchFrom.Contains(peerToBlacklist))
}

// There are multiple needed blocks and Ancestors returns all at once
func TestBootstrapperAncestors(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)
	blkID3 := ids.Empty.Prefix(3)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}
	blkBytes3 := []byte{3}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Processing,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}

	vm.CantSetState = false
	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		require.Equal(t, blk0.ID(), blkID)
		return blk0, nil
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{blkID3}

	parsedBlk1 := false
	parsedBlk2 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, database.ErrNotFound
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, database.ErrNotFound
		case blkID3:
			return blk3, nil
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		case bytes.Equal(blkBytes, blkBytes3):
			return blk3, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestID := new(uint32)
	requested := ids.Empty
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch vtxID {
		case blkID1, blkID2:
		default:
			t.Fatalf("should have requested blk1 or blk2")
		}
		*requestID = reqID
		requested = vtxID
	}

	if err := bs.ForceAccepted(context.Background(), acceptedIDs); err != nil { // should request blk2
		t.Fatal(err)
	}

	if err := bs.Ancestors(context.Background(), peerID, *requestID, [][]byte{blkBytes2, blkBytes1}); err != nil { // respond with blk2 and blk1
		t.Fatal(err)
	} else if requested != blkID2 {
		t.Fatal("should not have requested another block")
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

func TestBootstrapperFinalized(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		require.Equal(t, blk0.ID(), blkID)
		return blk0, nil
	}
	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = false
	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	parsedBlk1 := false
	parsedBlk2 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, database.ErrNotFound
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, database.ErrNotFound
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	if err := bs.ForceAccepted(context.Background(), []ids.ID{blkID1, blkID2}); err != nil { // should request blk2 and blk1
		t.Fatal(err)
	}

	reqIDBlk2, ok := requestIDs[blkID2]
	if !ok {
		t.Fatalf("should have requested blk2")
	}

	if err := bs.Ancestors(context.Background(), peerID, reqIDBlk2, [][]byte{blkBytes2, blkBytes1}); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

func TestRestartBootstrapping(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)
	blkID3 := ids.Empty.Prefix(3)
	blkID4 := ids.Empty.Prefix(4)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}
	blkBytes3 := []byte{3}
	blkBytes4 := []byte{4}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Unknown,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}
	blk4 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID4,
			StatusV: choices.Unknown,
		},
		ParentV: blk3.IDV,
		HeightV: 4,
		BytesV:  blkBytes4,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	parsedBlk1 := false
	parsedBlk2 := false
	parsedBlk3 := false
	parsedBlk4 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, database.ErrNotFound
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, database.ErrNotFound
		case blkID3:
			if parsedBlk3 {
				return blk3, nil
			}
			return nil, database.ErrNotFound
		case blkID4:
			if parsedBlk4 {
				return blk4, nil
			}
			return nil, database.ErrNotFound
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		case bytes.Equal(blkBytes, blkBytes3):
			blk3.StatusV = choices.Processing
			parsedBlk3 = true
			return blk3, nil
		case bytes.Equal(blkBytes, blkBytes4):
			blk4.StatusV = choices.Processing
			parsedBlk4 = true
			return blk4, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	bsIntf, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	bs, ok := bsIntf.(*bootstrapper)
	if !ok {
		t.Fatal("unexpected bootstrapper type")
	}

	vm.CantSetState = false
	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	// Force Accept blk3
	if err := bs.ForceAccepted(context.Background(), []ids.ID{blkID3}); err != nil { // should request blk3
		t.Fatal(err)
	}

	reqID, ok := requestIDs[blkID3]
	if !ok {
		t.Fatalf("should have requested blk3")
	}

	if err := bs.Ancestors(context.Background(), peerID, reqID, [][]byte{blkBytes3, blkBytes2}); err != nil {
		t.Fatal(err)
	}

	if _, ok := requestIDs[blkID1]; !ok {
		t.Fatal("should have requested blk1")
	}

	// Remove request, so we can restart bootstrapping via ForceAccepted
	if removed := bs.OutstandingRequests.RemoveAny(blkID1); !removed {
		t.Fatal("Expected to find an outstanding request for blk1")
	}
	requestIDs = map[ids.ID]uint32{}

	if err := bs.ForceAccepted(context.Background(), []ids.ID{blkID4}); err != nil {
		t.Fatal(err)
	}

	blk1RequestID, ok := requestIDs[blkID1]
	if !ok {
		t.Fatal("should have re-requested blk1 on restart")
	}
	blk4RequestID, ok := requestIDs[blkID4]
	if !ok {
		t.Fatal("should have requested blk4 as new accepted frontier")
	}

	if err := bs.Ancestors(context.Background(), peerID, blk1RequestID, [][]byte{blkBytes1}); err != nil {
		t.Fatal(err)
	}

	if config.Ctx.GetState() == snow.NormalOp {
		t.Fatal("Bootstrapping should not have finished with outstanding request for blk4")
	}

	if err := bs.Ancestors(context.Background(), peerID, blk4RequestID, [][]byte{blkBytes4}); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk3.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk4.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

func TestBootstrapOldBlockAfterStateSync(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 0,
		BytesV:  utils.RandomBytes(32),
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  utils.RandomBytes(32),
	}

	vm.LastAcceptedF = func() (ids.ID, error) { return blk1.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blk0.ID():
			return nil, database.ErrNotFound
		case blk1.ID():
			return blk1, nil
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blk0.Bytes()):
			return blk0, nil
		case bytes.Equal(blkBytes, blk1.Bytes()):
			return blk1, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	bsIntf, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	bs, ok := bsIntf.(*bootstrapper)
	if !ok {
		t.Fatal("unexpected bootstrapper type")
	}

	vm.CantSetState = false
	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	// Force Accept, the already transitively accepted, blk0
	if err := bs.ForceAccepted(context.Background(), []ids.ID{blk0.ID()}); err != nil { // should request blk0
		t.Fatal(err)
	}

	reqID, ok := requestIDs[blk0.ID()]
	if !ok {
		t.Fatalf("should have requested blk0")
	}

	if err := bs.Ancestors(context.Background(), peerID, reqID, [][]byte{blk0.Bytes()}); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Processing:
		t.Fatalf("Block should be processing")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

func TestBootstrapContinueAfterHalt(t *testing.T) {
	config, _, _, vm := newConfig(t)

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  utils.RandomBytes(32),
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  utils.RandomBytes(32),
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  utils.RandomBytes(32),
	}

	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }

	bsIntf, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	bs, ok := bsIntf.(*bootstrapper)
	if !ok {
		t.Fatal("unexpected bootstrapper type")
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blk0.ID():
			return blk0, nil
		case blk1.ID():
			bs.Halt()
			return blk1, nil
		case blk2.ID():
			return blk2, nil
		default:
			t.Fatal(database.ErrNotFound)
			panic(database.ErrNotFound)
		}
	}

	vm.CantSetState = false
	if err := bs.Start(0); err != nil {
		t.Fatal(err)
	}

	if err := bs.ForceAccepted(context.Background(), []ids.ID{blk2.ID()}); err != nil {
		t.Fatal(err)
	}

	if bs.Blocked.NumMissingIDs() != 1 {
		t.Fatal("Should have left blk1 as missing")
	}
}
