// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap/interval"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var errUnknownBlock = errors.New("unknown block")

func newConfig(t *testing.T) (Config, ids.NodeID, *enginetest.Sender, *blocktest.VM, func()) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	vdrs := validators.NewManager()

	sender := &enginetest.Sender{}
	vm := &blocktest.VM{}

	sender.T = t
	vm.T = t

	sender.Default(true)
	vm.Default(true)

	isBootstrapped := false
	bootstrapTracker := &enginetest.BootstrapTracker{
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

	totalWeight, err := vdrs.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupTracker := tracker.NewStartup(tracker.NewPeers(), totalWeight/2+1)
	vdrs.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)

	require.NoError(startupTracker.Connected(context.Background(), peer, version.CurrentApp))

	snowGetHandler, err := getter.New(vm, sender, ctx.Log, time.Second, 2000, ctx.Registerer)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"",
		prometheus.NewRegistry(),
		nil,
		nil,
	)
	require.NoError(err)

	peerTracker.Connected(peer, version.CurrentApp)

	var halter common.Halter

	return Config{
		Haltable:                       &common.Halter{},
		NonVerifyingParse:              vm.ParseBlock,
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		SampleK:                        vdrs.NumValidators(ctx.SubnetID),
		StartupTracker:                 startupTracker,
		PeerTracker:                    peerTracker,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		AncestorsMaxContainersReceived: 2000,
		DB:                             memdb.New(),
		VM:                             vm,
	}, peer, sender, vm, halter.Halt
}

func TestBootstrapperStartsOnlyIfEnoughStakeIsConnected(t *testing.T) {
	require := require.New(t)

	sender := &enginetest.Sender{T: t}
	vm := &blocktest.VM{
		VM: enginetest.VM{T: t},
	}

	sender.Default(true)
	vm.Default(true)
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	// create bootstrapper configuration
	peers := validators.NewManager()
	sampleK := 2
	alpha := uint64(10)
	startupAlpha := alpha

	startupTracker := tracker.NewStartup(tracker.NewPeers(), startupAlpha)
	peers.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)

	snowGetHandler, err := getter.New(vm, sender, ctx.Log, time.Second, 2000, ctx.Registerer)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"",
		prometheus.NewRegistry(),
		nil,
		nil,
	)
	require.NoError(err)

	cfg := Config{
		Haltable:                       &common.Halter{},
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        peers,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		PeerTracker:                    peerTracker,
		Sender:                         sender,
		BootstrapTracker:               &enginetest.BootstrapTracker{},
		AncestorsMaxContainersReceived: 2000,
		DB:                             memdb.New(),
		VM:                             vm,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	// create bootstrapper
	dummyCallback := func(context.Context, uint32) error {
		cfg.Ctx.State.Set(snow.EngineState{
			Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
			State: snow.NormalOp,
		})
		return nil
	}
	bs, err := New(cfg, dummyCallback)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

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

	peerTracker.Connected(vdr0, version.CurrentApp)
	require.NoError(bs.Connected(context.Background(), vdr0, version.CurrentApp))

	require.NoError(bs.Start(context.Background(), 0))
	require.False(frontierRequested)

	// finally attempt starting bootstrapper with enough stake connected. Frontiers should be requested.
	vdr := ids.GenerateTestNodeID()
	require.NoError(peers.AddStaker(ctx.SubnetID, vdr, nil, ids.Empty, startupAlpha))

	peerTracker.Connected(vdr, version.CurrentApp)
	require.NoError(bs.Connected(context.Background(), vdr, version.CurrentApp))
	require.True(frontierRequested)
}

// Single node in the accepted frontier; no need to fetch parent
func TestBootstrapperSingleFrontier(t *testing.T) {
	require := require.New(t)

	config, _, _, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(1)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[0:1])))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

// Requests the unknown block and gets back a Ancestors with unexpected block.
// Requests again and gets the expected block.
func TestBootstrapperUnknownByzantineResponse(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(2)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	var requestID uint32
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		require.Equal(blks[1].ID(), blkID)
		requestID = reqID
	}

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[1:2]))) // should request blk1

	oldReqID := requestID
	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, blocksToBytes(blks[0:1]))) // respond with wrong block
	require.NotEqual(oldReqID, requestID)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, blocksToBytes(blks[1:2])))

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	snowmantest.RequireStatusIs(require, snowtest.Accepted, blks...)

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[1:2])))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

// There are multiple needed blocks and multiple Ancestors are required
func TestBootstrapperPartialFetch(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(4)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	var (
		requestID uint32
		requested ids.ID
	)
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		require.Contains([]ids.ID{blks[1].ID(), blks[3].ID()}, blkID)
		requestID = reqID
		requested = blkID
	}

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[3:4]))) // should request blk3
	require.Equal(blks[3].ID(), requested)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, blocksToBytes(blks[2:4]))) // respond with blk3 and blk2
	require.Equal(blks[1].ID(), requested)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, blocksToBytes(blks[1:2]))) // respond with blk1

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	snowmantest.RequireStatusIs(require, snowtest.Accepted, blks...)

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[3:4])))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

// There are multiple needed blocks and some validators do not have all the
// blocks.
func TestBootstrapperEmptyResponse(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(2)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	var (
		requestedNodeID ids.NodeID
		requestID       uint32
	)
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(blks[1].ID(), blkID)
		requestedNodeID = nodeID
		requestID = reqID
	}

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[1:2])))
	require.Equal(requestedNodeID, peerID)

	// Add another peer to allow a new node to be selected. A new node should be
	// sampled if the prior response was empty.
	bs.PeerTracker.Connected(ids.GenerateTestNodeID(), version.CurrentApp)

	require.NoError(bs.Ancestors(context.Background(), requestedNodeID, requestID, nil)) // respond with empty
	require.NotEqual(requestedNodeID, peerID)

	require.NoError(bs.Ancestors(context.Background(), requestedNodeID, requestID, blocksToBytes(blks[1:2])))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	snowmantest.RequireStatusIs(require, snowtest.Accepted, blks...)
}

// There are multiple needed blocks and Ancestors returns all at once
func TestBootstrapperAncestors(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(4)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	var (
		requestID uint32
		requested ids.ID
	)
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		require.Equal(blks[3].ID(), blkID)
		requestID = reqID
		requested = blkID
	}

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[3:4]))) // should request blk3
	require.Equal(blks[3].ID(), requested)

	require.NoError(bs.Ancestors(context.Background(), peerID, requestID, blocksToBytes(blks))) // respond with all the blocks

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	snowmantest.RequireStatusIs(require, snowtest.Accepted, blks...)

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[3:4])))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

func TestBootstrapperFinalized(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(3)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		requestIDs[blkID] = reqID
	}

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[1:3]))) // should request blk1 and blk2

	reqIDBlk2, ok := requestIDs[blks[2].ID()]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqIDBlk2, blocksToBytes(blks[1:3])))

	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	snowmantest.RequireStatusIs(require, snowtest.Accepted, blks...)

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[2:3])))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

func TestRestartBootstrapping(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(5)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		requestIDs[blkID] = reqID
	}

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[3:4]))) // should request blk3

	reqID, ok := requestIDs[blks[3].ID()]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqID, blocksToBytes(blks[2:4])))
	require.Contains(requestIDs, blks[1].ID())

	// Remove request, so we can restart bootstrapping via startSyncing
	_, removed := bs.outstandingRequests.DeleteValue(blks[1].ID())
	require.True(removed)
	clear(requestIDs)

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[4:5])))

	blk1RequestID, ok := requestIDs[blks[1].ID()]
	require.True(ok)
	blk4RequestID, ok := requestIDs[blks[4].ID()]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, blk1RequestID, blocksToBytes(blks[1:2])))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	require.Equal(snowtest.Accepted, blks[0].Status)
	snowmantest.RequireStatusIs(require, snowtest.Undecided, blks[1:]...)

	require.NoError(bs.Ancestors(context.Background(), peerID, blk4RequestID, blocksToBytes(blks[4:5])))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	snowmantest.RequireStatusIs(require, snowtest.Accepted, blks...)

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[4:5])))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
}

func TestBootstrapOldBlockAfterStateSync(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(2)
	initializeVMWithBlockchain(vm, blks)

	blks[0].Status = snowtest.Undecided
	require.NoError(blks[1].Accept(context.Background()))

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		requestIDs[blkID] = reqID
	}

	// Force Accept, the already transitively accepted, blk0
	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[0:1]))) // should request blk0

	reqID, ok := requestIDs[blks[0].ID()]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqID, blocksToBytes(blks[0:1])))
	require.Equal(snow.NormalOp, config.Ctx.State.Get().State)
	require.Equal(snowtest.Undecided, blks[0].Status)
	require.Equal(snowtest.Accepted, blks[1].Status)
}

func TestBootstrapContinueAfterHalt(t *testing.T) {
	require := require.New(t)

	config, _, _, vm, halt := newConfig(t)

	blks := snowmantest.BuildChain(2)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	getBlockF := vm.GetBlockF
	vm.GetBlockF = func(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
		halt()
		return getBlockF(ctx, blkID)
	}

	require.NoError(bs.Start(context.Background(), 0))

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[1:2])))
	require.Equal(1, bs.missingBlockIDs.Len())
}

func TestBootstrapNoParseOnNew(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	peers := validators.NewManager()

	sender := &enginetest.Sender{}
	vm := &blocktest.VM{}

	sender.T = t
	vm.T = t

	sender.Default(true)
	vm.Default(true)

	isBootstrapped := false
	bootstrapTracker := &enginetest.BootstrapTracker{
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

	totalWeight, err := peers.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupTracker := tracker.NewStartup(tracker.NewPeers(), totalWeight/2+1)
	peers.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)
	require.NoError(startupTracker.Connected(context.Background(), peer, version.CurrentApp))

	snowGetHandler, err := getter.New(vm, sender, ctx.Log, time.Second, 2000, ctx.Registerer)
	require.NoError(err)

	blk1 := snowmantest.BuildChild(snowmantest.Genesis)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}

	intervalDB := memdb.New()
	tree, err := interval.NewTree(intervalDB)
	require.NoError(err)
	_, err = interval.Add(intervalDB, tree, 0, blk1.Height(), blk1.Bytes())
	require.NoError(err)

	vm.GetBlockF = nil

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"",
		prometheus.NewRegistry(),
		nil,
		nil,
	)
	require.NoError(err)

	peerTracker.Connected(peer, version.CurrentApp)

	config := Config{
		Haltable:                       &common.Halter{},
		AllGetsServer:                  snowGetHandler,
		Ctx:                            ctx,
		Beacons:                        peers,
		SampleK:                        peers.NumValidators(ctx.SubnetID),
		StartupTracker:                 startupTracker,
		PeerTracker:                    peerTracker,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		AncestorsMaxContainersReceived: 2000,
		DB:                             intervalDB,
		VM:                             vm,
	}

	_, err = New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
}

func TestBootstrapperReceiveStaleAncestorsMessage(t *testing.T) {
	require := require.New(t)

	config, peerID, sender, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(3)
	initializeVMWithBlockchain(vm, blks)

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	require.NoError(err)
	bs.TimeoutRegistrar = &enginetest.Timer{}

	require.NoError(bs.Start(context.Background(), 0))

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(_ context.Context, nodeID ids.NodeID, reqID uint32, blkID ids.ID) {
		require.Equal(peerID, nodeID)
		requestIDs[blkID] = reqID
	}

	require.NoError(bs.startSyncing(context.Background(), blocksToIDs(blks[1:3]))) // should request blk1 and blk2

	reqIDBlk1, ok := requestIDs[blks[1].ID()]
	require.True(ok)
	reqIDBlk2, ok := requestIDs[blks[2].ID()]
	require.True(ok)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqIDBlk2, blocksToBytes(blks[1:3])))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
	snowmantest.RequireStatusIs(require, snowtest.Accepted, blks...)

	require.NoError(bs.Ancestors(context.Background(), peerID, reqIDBlk1, blocksToBytes(blks[1:2])))
	require.Equal(snow.Bootstrapping, config.Ctx.State.Get().State)
}

func TestBootstrapperRollbackOnSetState(t *testing.T) {
	require := require.New(t)

	config, _, _, vm, _ := newConfig(t)

	blks := snowmantest.BuildChain(2)
	initializeVMWithBlockchain(vm, blks)

	blks[1].Status = snowtest.Accepted

	bs, err := New(
		config,
		func(context.Context, uint32) error {
			config.Ctx.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				State: snow.NormalOp,
			})
			return nil
		},
	)
	bs.TimeoutRegistrar = &enginetest.Timer{}
	require.NoError(err)

	vm.SetStateF = func(context.Context, snow.State) error {
		blks[1].Status = snowtest.Undecided
		return nil
	}

	require.NoError(bs.Start(context.Background(), 0))
	require.Equal(blks[0].HeightV, bs.startingHeight)
}

func initializeVMWithBlockchain(vm *blocktest.VM, blocks []*snowmantest.Block) {
	vm.CantSetState = false
	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		blocks,
	)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		for _, blk := range blocks {
			if blk.Status == snowtest.Accepted && blk.ID() == blkID {
				return blk, nil
			}
		}
		return nil, database.ErrNotFound
	}
	vm.ParseBlockF = func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
		for _, blk := range blocks {
			if bytes.Equal(blk.Bytes(), blkBytes) {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}
}

func blocksToIDs(blocks []*snowmantest.Block) []ids.ID {
	blkIDs := make([]ids.ID, len(blocks))
	for i, blk := range blocks {
		blkIDs[i] = blk.ID()
	}
	return blkIDs
}

func blocksToBytes(blocks []*snowmantest.Block) [][]byte {
	numBlocks := len(blocks)
	blkBytes := make([][]byte, numBlocks)
	for i, blk := range blocks {
		blkBytes[numBlocks-i-1] = blk.Bytes()
	}
	return blkBytes
}
