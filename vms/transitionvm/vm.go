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
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ Chain = (*VM)(nil)

type Chain interface {
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.SetPreferenceWithContextChainVM
	block.StateSyncableVM
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

	// vm state
	transitionLock sync.RWMutex
	transitioned   bool
	consensusState snow.State
	connections    *connections
	httpHandlers   *httpHandlers
	current        *current
}

// current contains all of the chain specific values. When initializing and
// transitioning, all of these values are overridden.
type current struct {
	chain    Chain
	requests *requests

	ctx       context.Context
	ctxCancel context.CancelFunc
}

var transitionedKey = prefixdb.MakePrefix([]byte("transitioned"))

func (vm *VM) Initialize(
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

	vm.chainCtx = &snow.Context{
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
	vm.db = db
	vm.genesisBytes = genesisBytes
	vm.upgradeBytes = upgradeBytes
	vm.configBytes = configBytes
	vm.fxs = fxs
	vm.appSender = appSender

	vm.connections = newConnections()
	vm.httpHandlers = newHTTPHandlers()

	vm.current = &current{
		chain: vm.preTransitionChain,
	}

	chainCtx.Log.Info("checking for last synchronous block")
	has, err := vm.db.Has(transitionedKey)
	if err != nil {
		return fmt.Errorf("checking for last synchronous block: %w", err)
	}

	if has {
		chainCtx.Log.Info("initializing post-transition VM")
		if err := vm.initChain(ctx, vm.postTransitionChain, vm.chainCtx); err != nil {
			return fmt.Errorf("initializing post-transition VM: %w", err)
		}
		vm.transitioned = true
		return nil
	}

	chainCtx.Log.Info("initializing pre-transition VM")
	if err := vm.initChain(ctx, vm.preTransitionChain, chainCtx); err != nil {
		return fmt.Errorf("initializing pre-transition VM: %w", err)
	}

	// It's possible we crashed between accepting the last block and writing the
	// transition flag. Or maybe the genesis block was the transition block.
	lastAcceptedID, err := vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("loading last accepted ID: %w", err)
	}
	lastAccepted, err := vm.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return fmt.Errorf("loading last accepted block %s: %w", lastAcceptedID, err)
	}
	if time := lastAccepted.Timestamp(); time.Before(vm.transitionTime) {
		return nil
	}
	return vm.transition(ctx, lastAccepted)
}

func (vm *VM) transition(ctx context.Context, last snowman.Block) error {
	// We must cancel the context before grabbing the lock to ensure that
	// [VM.WaitForEvent] does not block indefinitely.
	vm.current.ctxCancel()

	vm.transitionLock.Lock()
	defer vm.transitionLock.Unlock()

	lastID := last.ID()
	vm.chainCtx.Log.Info("transitioning VMs",
		zap.Stringer("lastID", lastID),
		zap.Uint64("lastHeight", last.Height()),
		zap.Time("lastTime", last.Timestamp()),
	)

	vm.chainCtx.Log.Info("shutting down pre-transition VM")
	if err := vm.preTransitionChain.Shutdown(ctx); err != nil {
		return fmt.Errorf("closing pre-transition chain: %w", err)
	}

	vm.chainCtx.Log.Info("writing last synchronous block")
	if err := vm.db.Put(transitionedKey, nil); err != nil {
		return fmt.Errorf("saving last synchronous block: %w", err)
	}

	vm.chainCtx.Log.Info("initializing post-transition VM")
	if err := vm.initChain(ctx, vm.postTransitionChain, vm.chainCtx); err != nil {
		return fmt.Errorf("initializing post-transition VM: %w", err)
	}

	vm.chainCtx.Log.Info("initializing post-transition VM preference",
		zap.Stringer("blkID", lastID),
	)
	if err := vm.postTransitionChain.SetPreference(ctx, lastID); err != nil {
		return fmt.Errorf("setting post-transition preference: %w", err)
	}

	vm.transitioned = true
	vm.chainCtx.Log.Info("transition finished successfully")
	return nil
}

func (vm *VM) initChain(ctx context.Context, chain Chain, chainCtx *snow.Context) error {
	var (
		requests requests
		sender   = sender{
			AppSender: vm.appSender,
			requests:  &requests,
		}
	)
	err := chain.Initialize(
		ctx,
		chainCtx,
		vm.db,
		vm.genesisBytes,
		vm.upgradeBytes,
		vm.configBytes,
		vm.fxs,
		&sender,
	)
	if err != nil {
		return fmt.Errorf("initializing chain: %w", err)
	}

	if vm.consensusState != snow.Initializing {
		if err := chain.SetState(ctx, vm.consensusState); err != nil {
			return fmt.Errorf("setting consensus state: %w", err)
		}
	}
	if err := vm.connections.reconnect(ctx, chain); err != nil {
		return fmt.Errorf("reconnecting to vm: %w", err)
	}

	newHandlers, err := chain.CreateHandlers(ctx)
	if err != nil {
		return fmt.Errorf("creating http handlers: %w", err)
	}
	vm.httpHandlers.set(newHandlers)

	ctx, ctxCancel := context.WithCancel(context.Background())
	vm.current = &current{
		chain:     chain,
		requests:  &requests,
		ctx:       ctx,
		ctxCancel: ctxCancel,
	}
	return nil
}
