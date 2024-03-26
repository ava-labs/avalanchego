// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap/interval"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var errUnknownBlock = errors.New("unknown block")

func newConfig(t *testing.T) (Config, ids.NodeID, *common.SenderTest, *block.TestVM) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	vdrs := validators.NewManager()

	sender := &common.SenderTest{}
	vm := &block.TestVM{}

	sender.T = t
	vm.T = t

	sender.Default(true)
	vm.Default(true)

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

	sender.CantSendGetAcceptedFrontier = false

	peer := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, peer, nil, ids.Empty, 1))

	peerTracker := tracker.NewPeers()
	totalWeight, err := vdrs.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupTracker := tracker.NewStartup(peerTracker, totalWeight/2+1)
	vdrs.RegisterCallbackListener(ctx.SubnetID, startupTracker)

	require.NoError(startupTracker.Connected(context.Background(), peer, version.CurrentApp))

	snowGetHandler, err := getter.New(vm, sender, ctx.Log, time.Second, 2000, ctx.Registerer)
	require.NoError(err)

	return Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		SampleK:                        vdrs.Count(ctx.SubnetID),
		StartupTracker:                 startupTracker,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersReceived: 2000,
		DB:                             memdb.New(),
		VM:                             vm,
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
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	// create boostrapper configuration
	peers := validators.NewManager()
	sampleK := 2
	alpha := uint64(10)
	startupAlpha := alpha

	peerTracker := tracker.NewPeers()
	startupTracker := tracker.NewStartup(peerTracker, startupAlpha)
	peers.RegisterCallbackListener(ctx.SubnetID, startupTracker)

	snowGetHandler, err := getter.New(vm, sender, ctx.Log, time.Second, 2000, ctx.Registerer)
	require.NoError(err)
	cfg := Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        peers,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         sender,
		BootstrapTracker:               &common.BootstrapTrackerTest{},
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersReceived: 2000,
		DB:                             memdb.New(),
		VM:                             vm,
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
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blk0.ID(), nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(blk0.ID(), blkID)
		return blk0, nil
	}

	// create bootstrapper
	dummyCallback := func(context.Context, uint32) error {
		cfg.Ctx.State.Set(snow.EngineState{
			Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
			State: snow.NormalOp,
		})
		return nil
	}
	bs, err := New(cfg, dummyCallback)
	require.NoError(err)

	vm.CantSetState = false
	vm.CantConnected = true
	vm.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}

	frontierRequested := false
	sender.CantSendGetAcceptedFrontier = false
	sender.SendGetAcceptedFrontierF = func(context.Context, set.Set[ids.NodeID], uint32) {
		frontierRequested = true
	}

	// attempt starting bootstrapper with no stake connected. Bootstrapper should stall.
	require.NoError(bs.Start(context.Background(), 0))
	require.False(frontierRequested)

	// attempt starting bootstrapper with not enough stake connected. Bootstrapper should stall.
	vdr0 := ids.GenerateTestNodeID()
	require.NoError(peers.AddStaker(ctx.SubnetID, vdr0, nil, ids.Empty, startupAlpha/2))
	require.NoError(bs.Connected(context.Background(), vdr0, version.CurrentApp))

	require.NoError(bs.Start(context.Background(), 0))
	require.False(frontierRequested)

	// finally attempt starting bootstrapper with enough stake connected. Frontiers should be requested.
	vdr := ids.GenerateTestNodeID()
	require.NoError(peers.AddStaker(ctx.SubnetID, vdr, nil, ids.Empty, startupAlpha))
	require.NoError(bs.Connected(context.Background(), vdr, version.CurrentApp))
	require.True(frontierRequested)
}

// Single node in the accepted frontier; no need to fetch parent
func TestBootstrapperSingleFrontier(t *testing.T) {
	require := require.New(t)

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
	blks := map[ids.ID]snowman.Block{
		blkID0: blk0,
		blkID1: blk1,
	}

	vm.CantSetState = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		var (
			lastAcceptedID     = blk0.ID()
			lastAcceptedHeight = blk0.Height()
		)
		for blkID, blk := range blks {
			height := blk.Height()
			if blk.Status() == choices.Accepted && height > lastAcceptedHeight {
				lastAcceptedID = blkID
				lastAcceptedHeight = height
			}
		}
		return lastAcceptedID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blk, ok := blks[blkID]; ok {
			return blk, nil
		}
		return nil, database.ErrNotFound
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blk := range blks {
			if bytes.Equal(blk.Bytes(), blkBytes) {
				return blk, nil
			}
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	require.NoError(bs.Start(context.Background(), 0))

	acceptedIDs := []ids.ID{blkID1}
	require.NoError(bs.startSyncing(context.Background(), acceptedIDs))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk1.Status())
}

// Requests the unknown block and gets back a Ancestors with unexpected block.
// Requests again and gets the expected block.
func TestBootstrapperUnknownByzantineResponse(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm := newConfig(t)

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
	blks := map[ids.ID]snowman.Block{
		blkID0: blk0,
		blkID1: blk1,
	}

	vm.CantSetState = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		var (
			lastAcceptedID     = blk0.ID()
			lastAcceptedHeight = blk0.Height()
		)
		for blkID, blk := range blks {
			height := blk.Height()
			if blk.Status() == choices.Accepted && height > lastAcceptedHeight {
				lastAcceptedID = blkID
				lastAcceptedHeight = height
			}
		}
		return lastAcceptedID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blk, ok := blks[blkID]; ok && blk.Status() == choices.Accepted {
			return blk, nil
		}
		return nil, database.ErrNotFound
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blk := range blks {
			if bytes.Equal(blk.Bytes(), blkBytes) {
				return blk, nil
			}
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	require.NoError(bs.Start(context.Background(), 0))

	var requestID uint32
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, vdr)
		require.Equal(blkID1, blkID)
		requestID = reqID
	}

	vm.CantSetState = false
	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID1})) // should request blk1

	oldReqID := requestID
	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, [][]byte{blkBytes0})) // respond with wrong block
	require.NotEqual(oldReqID, requestID)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, [][]byte{blkBytes1}))

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())

	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID1}))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

// There are multiple needed blocks and Ancestors returns one at a time
func TestBootstrapperPartialFetch(t *testing.T) {
	require := require.New(t)

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
			StatusV: choices.Processing,
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
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Processing,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}
	blks := map[ids.ID]snowman.Block{
		blkID0: blk0,
		blkID1: blk1,
		blkID2: blk2,
		blkID3: blk3,
	}

	vm.CantSetState = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		var (
			lastAcceptedID     = blk0.ID()
			lastAcceptedHeight = blk0.Height()
		)
		for blkID, blk := range blks {
			height := blk.Height()
			if blk.Status() == choices.Accepted && height > lastAcceptedHeight {
				lastAcceptedID = blkID
				lastAcceptedHeight = height
			}
		}
		return lastAcceptedID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blk, ok := blks[blkID]; ok && blk.Status() == choices.Accepted {
			return blk, nil
		}
		return nil, database.ErrNotFound
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blk := range blks {
			if bytes.Equal(blk.Bytes(), blkBytes) {
				return blk, nil
			}
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	require.NoError(bs.Start(context.Background(), 0))

	var (
		requestID uint32
		requested ids.ID
	)
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		require.Contains([]ids.ID{blkID1, blkID2, blkID3}, blkID)
		requestID = reqID
		requested = blkID
	}

	acceptedIDs := []ids.ID{blkID3}
	require.NoError(bs.startSyncing(context.Background(), acceptedIDs)) // should request blk3
	require.Equal(blkID3, requested)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, [][]byte{blkBytes3, blkBytes2})) // respond with blk3 and blk2
	require.Equal(blkID1, requested)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, [][]byte{blkBytes1})) // respond with blk1

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())
	require.Equal(choices.Accepted, blk2.Status())

	require.NoError(bs.startSyncing(context.Background(), acceptedIDs))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

// There are multiple needed blocks and some validators do not have all the
// blocks.
func TestBootstrapperEmptyResponse(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm := newConfig(t)

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
	blks := map[ids.ID]snowman.Block{
		blkID0: blk0,
		blkID1: blk1,
	}

	vm.CantSetState = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		var (
			lastAcceptedID     = blk0.ID()
			lastAcceptedHeight = blk0.Height()
		)
		for blkID, blk := range blks {
			height := blk.Height()
			if blk.Status() == choices.Accepted && height > lastAcceptedHeight {
				lastAcceptedID = blkID
				lastAcceptedHeight = height
			}
		}
		return lastAcceptedID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blk, ok := blks[blkID]; ok && blk.Status() == choices.Accepted {
			return blk, nil
		}
		return nil, database.ErrNotFound
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blk := range blks {
			if bytes.Equal(blk.Bytes(), blkBytes) {
				return blk, nil
			}
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	require.NoError(bs.Start(context.Background(), 0))

	var (
		requestedNodeID  ids.NodeID
		requestID        uint32
		requestedBlockID ids.ID
	)
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(blkID1, blkID)
		requestedNodeID = nodeID
		requestID = reqID
		requestedBlockID = blkID
	}

	acceptedIDs := []ids.ID{blkID1}
	require.NoError(bs.startSyncing(context.Background(), acceptedIDs)) // should request blk3
	require.Equal(requestedNodeID, peerID)
	require.Equal(blkID1, requestedBlockID)

	// add another 2 validators to the fetch set to test behavior on empty
	// response
	bs.fetchFrom.Add(ids.GenerateTestNodeID(), ids.GenerateTestNodeID())

	require.NoError(bs.Ancestors(context.Background(), requestedNodeID, requestID, nil)) // respond with empty
	require.NotEqual(requestedNodeID, peerID)
	require.Equal(blkID1, requestedBlockID)

	require.NoError(bs.Ancestors(context.Background(), requestedNodeID, requestID, [][]byte{blkBytes1}))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())

	// check that peerID was removed from the fetch set
	require.NotContains(bs.fetchFrom, peerID)
}

// There are multiple needed blocks and Ancestors returns all at once
func TestBootstrapperAncestors(t *testing.T) {
	require := require.New(t)

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
			StatusV: choices.Processing,
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
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Processing,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}
	blks := map[ids.ID]snowman.Block{
		blkID0: blk0,
		blkID1: blk1,
		blkID2: blk2,
		blkID3: blk3,
	}

	vm.CantSetState = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		var (
			lastAcceptedID     = blk0.ID()
			lastAcceptedHeight = blk0.Height()
		)
		for blkID, blk := range blks {
			height := blk.Height()
			if blk.Status() == choices.Accepted && height > lastAcceptedHeight {
				lastAcceptedID = blkID
				lastAcceptedHeight = height
			}
		}
		return lastAcceptedID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blk, ok := blks[blkID]; ok && blk.Status() == choices.Accepted {
			return blk, nil
		}
		return nil, database.ErrNotFound
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blk := range blks {
			if bytes.Equal(blk.Bytes(), blkBytes) {
				return blk, nil
			}
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	require.NoError(bs.Start(context.Background(), 0))

	var (
		requestID uint32
		requested ids.ID
	)
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, vdr)
		require.Equal(blkID3, blkID)
		requestID = reqID
		requested = blkID
	}

	acceptedIDs := []ids.ID{blkID3}
	require.NoError(bs.startSyncing(context.Background(), acceptedIDs)) // should request blk3
	require.Equal(blkID3, requested)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, [][]byte{blkBytes3, blkBytes2, blkBytes1, blkBytes0})) // respond with all the blocks

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())
	require.Equal(choices.Accepted, blk2.Status())
	require.Equal(choices.Accepted, blk3.Status())

	require.NoError(bs.startSyncing(context.Background(), acceptedIDs))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

func TestBootstrapperFinalized(t *testing.T) {
	require := require.New(t)

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
			StatusV: choices.Processing,
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
	blks := map[ids.ID]snowman.Block{
		blkID0: blk0,
		blkID1: blk1,
		blkID2: blk2,
	}

	vm.CantSetState = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		var (
			lastAcceptedID     = blk0.ID()
			lastAcceptedHeight = blk0.Height()
		)
		for blkID, blk := range blks {
			height := blk.Height()
			if blk.Status() == choices.Accepted && height > lastAcceptedHeight {
				lastAcceptedID = blkID
				lastAcceptedHeight = height
			}
		}
		return lastAcceptedID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blk, ok := blks[blkID]; ok && blk.Status() == choices.Accepted {
			return blk, nil
		}
		return nil, database.ErrNotFound
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blk := range blks {
			if bytes.Equal(blk.Bytes(), blkBytes) {
				return blk, nil
			}
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	require.NoError(bs.Start(context.Background(), 0))

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, vdr)
		requestIDs[blkID] = reqID
	}

	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID1, blkID2})) // should request blk1 and blk2

	reqIDBlk2, ok := requestIDs[blkID2]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqIDBlk2, [][]byte{blkBytes2, blkBytes1}))

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())
	require.Equal(choices.Accepted, blk2.Status())

	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID2}))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

func TestRestartBootstrapping(t *testing.T) {
	require := require.New(t)

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
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blk0.ID(), nil
	}
	parsedBlk1 := false
	parsedBlk2 := false
	parsedBlk3 := false
	parsedBlk4 := false
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
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
			require.FailNow(database.ErrNotFound.Error())
			return nil, database.ErrNotFound
		}
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
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
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	vm.CantSetState = false
	require.NoError(bs.Start(context.Background(), 0))

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, vdr)
		requestIDs[blkID] = reqID
	}

	// Force Accept blk3
	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID3})) // should request blk3

	reqID, ok := requestIDs[blkID3]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqID, [][]byte{blkBytes3, blkBytes2}))

	require.Contains(requestIDs, blkID1)

	// Remove request, so we can restart bootstrapping via startSyncing
	_, removed := bs.outstandingRequests.DeleteValue(blkID1)
	require.True(removed)
	requestIDs = map[ids.ID]uint32{}

	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID4}))

	blk1RequestID, ok := requestIDs[blkID1]
	require.True(ok)
	blk4RequestID, ok := requestIDs[blkID4]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, blk1RequestID, [][]byte{blkBytes1}))

	require.NotEqual(snow.NormalOp, config.Ctx.State.Get().State)

	require.NoError(bs.Ancestors(context.Background(), peerID, blk4RequestID, [][]byte{blkBytes4}))

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())
	require.Equal(choices.Accepted, blk2.Status())
	require.Equal(choices.Accepted, blk3.Status())
	require.Equal(choices.Accepted, blk4.Status())

	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID4}))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

func TestBootstrapOldBlockAfterStateSync(t *testing.T) {
	require := require.New(t)

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

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blk1.ID(), nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blk0.ID():
			return nil, database.ErrNotFound
		case blk1.ID():
			return blk1, nil
		default:
			require.FailNow(database.ErrNotFound.Error())
			return nil, database.ErrNotFound
		}
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blk0.Bytes()):
			return blk0, nil
		case bytes.Equal(blkBytes, blk1.Bytes()):
			return blk1, nil
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	vm.CantSetState = false
	require.NoError(bs.Start(context.Background(), 0))

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, vdr)
		requestIDs[blkID] = reqID
	}

	// Force Accept, the already transitively accepted, blk0
	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blk0.ID()})) // should request blk0

	reqID, ok := requestIDs[blk0.ID()]
	require.True(ok)
	require.NoError(bs.Ancestors(context.Background(), peerID, reqID, [][]byte{blk0.Bytes()}))

	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
	require.Equal(choices.Processing, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())
}

func TestBootstrapContinueAfterHalt(t *testing.T) {
	require := require.New(t)

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

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blk0.ID(), nil
	}

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blk0.ID():
			return blk0, nil
		case blk1.ID():
			bs.Halt(context.Background())
			return blk1, nil
		case blk2.ID():
			return blk2, nil
		default:
			require.FailNow(database.ErrNotFound.Error())
			return nil, database.ErrNotFound
		}
	}

	vm.CantSetState = false
	require.NoError(bs.Start(context.Background(), 0))

	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blk2.ID()}))

	require.Equal(1, bs.missingBlockIDs.Len())
}

func TestBootstrapNoParseOnNew(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	peers := validators.NewManager()

	sender := &common.SenderTest{}
	vm := &block.TestVM{}

	sender.T = t
	vm.T = t

	sender.Default(true)
	vm.Default(true)

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

	sender.CantSendGetAcceptedFrontier = false

	peer := ids.GenerateTestNodeID()
	require.NoError(peers.AddStaker(ctx.SubnetID, peer, nil, ids.Empty, 1))

	peerTracker := tracker.NewPeers()
	totalWeight, err := peers.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupTracker := tracker.NewStartup(peerTracker, totalWeight/2+1)
	peers.RegisterCallbackListener(ctx.SubnetID, startupTracker)
	require.NoError(startupTracker.Connected(context.Background(), peer, version.CurrentApp))

	snowGetHandler, err := getter.New(vm, sender, ctx.Log, time.Second, 2000, ctx.Registerer)
	require.NoError(err)

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
		ParentV: blk0.ID(),
		HeightV: 1,
		BytesV:  utils.RandomBytes(32),
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(blk0.ID(), blkID)
		return blk0, nil
	}

	intervalDB := memdb.New()
	tree, err := interval.NewTree(intervalDB)
	require.NoError(err)
	_, err = interval.Add(intervalDB, tree, 0, blk1.Height(), blk1.Bytes())
	require.NoError(err)

	vm.GetBlockF = nil

	config := Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        peers,
		SampleK:                        peers.Count(ctx.SubnetID),
		StartupTracker:                 startupTracker,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersReceived: 2000,
		DB:                             intervalDB,
		VM:                             vm,
	}

	_, err = New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
}

func TestBootstrapperReceiveStaleAncestorsMessage(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm := newConfig(t)

	var (
		blkID0    = ids.GenerateTestID()
		blkBytes0 = utils.RandomBytes(1024)
		blk0      = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     blkID0,
				StatusV: choices.Accepted,
			},
			HeightV: 0,
			BytesV:  blkBytes0,
		}

		blkID1    = ids.GenerateTestID()
		blkBytes1 = utils.RandomBytes(1024)
		blk1      = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     blkID1,
				StatusV: choices.Processing,
			},
			ParentV: blk0.IDV,
			HeightV: blk0.HeightV + 1,
			BytesV:  blkBytes1,
		}

		blkID2    = ids.GenerateTestID()
		blkBytes2 = utils.RandomBytes(1024)
		blk2      = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     blkID2,
				StatusV: choices.Processing,
			},
			ParentV: blk1.IDV,
			HeightV: blk1.HeightV + 1,
			BytesV:  blkBytes2,
		}
	)

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blk0.ID(), nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(blkID0, blkID)
		return blk0, nil
	}
	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)

	vm.CantSetState = false
	require.NoError(bs.Start(context.Background(), 0))

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if blk1.StatusV == choices.Accepted {
				return blk1, nil
			}
			return nil, database.ErrNotFound
		case blkID2:
			if blk2.StatusV == choices.Accepted {
				return blk2, nil
			}
			return nil, database.ErrNotFound
		default:
			require.FailNow(database.ErrNotFound.Error())
			return nil, database.ErrNotFound
		}
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			return blk2, nil
		default:
			require.FailNow(errUnknownBlock.Error())
			return nil, errUnknownBlock
		}
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, vdr ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, vdr)
		requestIDs[blkID] = reqID
	}

	require.NoError(bs.startSyncing(context.Background(), []ids.ID{blkID1, blkID2})) // should request blk2 and blk1

	reqIDBlk1, ok := requestIDs[blkID1]
	require.True(ok)
	reqIDBlk2, ok := requestIDs[blkID2]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqIDBlk2, [][]byte{blkBytes2, blkBytes1}))

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(choices.Accepted, blk0.Status())
	require.Equal(choices.Accepted, blk1.Status())
	require.Equal(choices.Accepted, blk2.Status())

	require.NoError(bs.Ancestors(context.Background(), peerID, reqIDBlk1, [][]byte{blkBytes1}))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
}
