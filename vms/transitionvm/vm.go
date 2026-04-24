// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
)

var _ Chain = (*VM)(nil)

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

	v.current = &current{
		chain:          v.preTransitionChain,
		consensusState: snow.Initializing,
		connections: &connections{
			nodes: make(map[ids.NodeID]*version.Application),
		},
		httpHandlers: &httpHandlers{
			routes: make(map[string]*httpHandler),
		},
	}

	chainCtx.Log.Info("checking for last synchronous block")
	has, err := state.HasLastSync(v.db)
	if err != nil {
		return fmt.Errorf("checking for last synchronous block: %w", err)
	}

	if has {
		chainCtx.Log.Info("initializing post-transition VM")
		if err := v.initChain(ctx, v.postTransitionChain, v.chainCtx); err != nil {
			return fmt.Errorf("initializing post-transition VM: %w", err)
		}
		v.transitioned = true
		return nil
	}

	chainCtx.Log.Info("initializing pre-transition VM")
	if err := v.initChain(ctx, v.preTransitionChain, chainCtx); err != nil {
		return fmt.Errorf("initializing pre-transition VM: %w", err)
	}

	// It's possible we crashed between accepting the last block and writing the
	// transition flag. Or maybe the genesis block was the transition block.
	lastAcceptedID, err := v.LastAccepted(ctx)
	lastAccepted, err := v.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return fmt.Errorf("loading last accepted block: %w", err)
	}
	if time := lastAccepted.Timestamp(); time.Before(v.transitionTime) {
		return nil
	}
	return v.transition(ctx, lastAccepted)
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
	if err := v.initChain(ctx, v.postTransitionChain, v.chainCtx); err != nil {
		return fmt.Errorf("initializing post-transition VM: %w", err)
	}

	v.chainCtx.Log.Info("initializing post-transition VM preference",
		zap.Stringer("blkID", lastID),
	)
	if err := v.postTransitionChain.SetPreference(ctx, lastID); err != nil {
		return fmt.Errorf("setting post-transition preference: %w", err)
	}

	v.transitioned = true
	v.chainCtx.Log.Info("transition finished successfully")
	return nil
}

func (v *VM) initChain(ctx context.Context, chain Chain, chainCtx *snow.Context) error {
	var (
		requests requests
		sender   = sender{
			AppSender: v.appSender,
			requests:  &requests,
		}
	)
	err := chain.Initialize(
		ctx,
		chainCtx,
		v.db,
		v.genesisBytes,
		v.upgradeBytes,
		v.configBytes,
		v.fxs,
		&sender,
	)
	if err != nil {
		return fmt.Errorf("initializing chain: %w", err)
	}

	if v.current.consensusState != snow.Initializing {
		if err := chain.SetState(ctx, v.current.consensusState); err != nil {
			return fmt.Errorf("setting consensus state: %w", err)
		}
	}
	if err := v.current.connections.reconnect(ctx, chain); err != nil {
		return fmt.Errorf("reconnecting to vm: %w", err)
	}

	newHandlers, err := chain.CreateHandlers(ctx)
	if err != nil {
		return fmt.Errorf("creating http handlers: %w", err)
	}
	v.current.httpHandlers.set(newHandlers)

	v.current.chain = chain
	v.current.requests = &requests
	v.current.ctx, v.current.ctxCancel = context.WithCancel(context.Background())
	return nil
}
