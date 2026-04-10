// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/saevm/state"
	"go.uber.org/zap"
)

var _ Chain = &VM{}

type Chain interface {
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.SetPreferenceWithContextChainVM
}

type VM struct {
	// transition parameters
	preTransitionChain  Chain
	postTransitionChain Chain
	transitionTime      time.Time

	// chain parameters
	chainCtx     *snow.Context // Has modified Lock and Metrics fields
	db           database.Database
	genesisBytes []byte
	upgradeBytes []byte
	configBytes  []byte
	fxs          []*common.Fx
	appSender    common.AppSender

	// current state
	transitionLock sync.RWMutex
	transitioned   bool
	current        *current
}

type current struct {
	chain          Chain
	consensusState snow.State
	requests       *requests
	connections    *connections
	httpHandlers   *httpHandlers

	ctx       context.Context
	ctxCancel context.CancelFunc
}

func (v *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	gatherer := metrics.NewPrefixGatherer()
	if err := chainCtx.Metrics.Register("transition", gatherer); err != nil {
		return err
	}

	v.chainCtx = &snow.Context{
		NetworkID:       chainCtx.NetworkID,
		SubnetID:        chainCtx.SubnetID,
		ChainID:         chainCtx.ChainID,
		NodeID:          chainCtx.NodeID,
		PublicKey:       chainCtx.PublicKey,
		NetworkUpgrades: chainCtx.NetworkUpgrades,
		XChainID:        chainCtx.XChainID,
		CChainID:        chainCtx.CChainID,
		AVAXAssetID:     chainCtx.AVAXAssetID,
		Log:             chainCtx.Log,
		Lock:            sync.RWMutex{},
		SharedMemory:    chainCtx.SharedMemory,
		BCLookup:        chainCtx.BCLookup,
		Metrics:         gatherer,
		WarpSigner:      chainCtx.WarpSigner,
		ValidatorState:  chainCtx.ValidatorState,
		ChainDataDir:    chainCtx.ChainDataDir,
	}
	v.db = db
	v.genesisBytes = genesisBytes
	v.upgradeBytes = upgradeBytes
	v.configBytes = configBytes
	v.fxs = fxs
	v.appSender = appSender

	var (
		preTransitionRequests requests
		preTransitionSender   = sender{
			AppSender: appSender,
			requests:  &preTransitionRequests,
		}
	)
	err := v.preTransitionChain.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		fxs,
		&preTransitionSender,
	)
	if err != nil {
		return fmt.Errorf("initializing pre-transition chain: %w", err)
	}

	stageContext, stageCancel := context.WithCancel(context.Background())
	v.current = &current{
		chain:          v.preTransitionChain,
		consensusState: snow.Initializing,
		requests:       &preTransitionRequests,
		connections: &connections{
			nodes: make(map[ids.NodeID]*version.Application),
		},
		httpHandlers: &httpHandlers{
			routes: make(map[string]*httpHandler),
		},
		ctx:       stageContext,
		ctxCancel: stageCancel,
	}
	return nil
}

func (v *VM) transition(ctx context.Context, last snowman.Block) error {
	// We must cancel the context before grabbing the lock to ensure that
	// [VM.WaitForEvent] does not block indefinitely.
	v.current.ctxCancel()

	v.transitionLock.Lock()
	defer v.transitionLock.Unlock()

	lastID := last.ID()
	lastBytes := last.Bytes()
	v.chainCtx.Log.Info("transitioning VMs",
		zap.Stringer("lastID", lastID),
		zap.Uint64("lastHeight", last.Height()),
		zap.Time("lastTime", last.Timestamp()),
	)

	v.chainCtx.Log.Info("shutting down pre-transition VM")
	if err := v.preTransitionChain.Shutdown(ctx); err != nil {
		return fmt.Errorf("closing pre-transition chain: %w", err)
	}

	v.chainCtx.Log.Info("writing last synchronous block")
	if err := state.WriteLastSync(v.db, lastBytes); err != nil {
		return fmt.Errorf("saving last synchronous block: %w", err)
	}

	v.chainCtx.Log.Info("initializing post-transition VM")
	var (
		postTransitionRequests requests
		postTransitionSender   = sender{
			AppSender: v.appSender,
			requests:  &postTransitionRequests,
		}
	)
	err := v.postTransitionChain.Initialize(
		ctx,
		v.chainCtx,
		v.db,
		v.genesisBytes,
		v.upgradeBytes,
		v.configBytes,
		v.fxs,
		&postTransitionSender,
	)
	if err != nil {
		return fmt.Errorf("initializing post-transition chain: %w", err)
	}

	v.chainCtx.Log.Info("initializing post-transition VM consensus state",
		zap.Stringer("state", v.current.consensusState),
	)
	if err := v.postTransitionChain.SetState(ctx, v.current.consensusState); err != nil {
		return fmt.Errorf("setting post-transition consensus state: %w", err)
	}

	v.chainCtx.Log.Info("initializing post-transition VM preference",
		zap.Stringer("state", v.current.consensusState),
	)
	if err := v.postTransitionChain.SetPreference(ctx, lastID); err != nil {
		return fmt.Errorf("setting post-transition preference: %w", err)
	}

	v.chainCtx.Log.Info("connecting post-transition VM to peers")
	if err := v.current.connections.reconnect(ctx, v.postTransitionChain); err != nil {
		return fmt.Errorf("reconnecting to post-transition vm: %w", err)
	}

	v.chainCtx.Log.Info("remapping http handlers to post-transition VM")
	newHandlers, err := v.postTransitionChain.CreateHandlers(ctx)
	if err != nil {
		return fmt.Errorf("creating post-ransition http handlers", err)
	}
	v.current.httpHandlers.set(newHandlers)

	v.current.chain = v.postTransitionChain
	v.current.requests = &postTransitionRequests
	v.current.ctx, v.current.ctxCancel = context.WithCancel(context.Background())
	v.transitioned = true

	v.chainCtx.Log.Info("transition finished successfully")
	return nil
}
