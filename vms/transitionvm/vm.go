// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package transitionvm implements a [block.ChainVM] that switches from one
// chain to another at a configured time.
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

// Chain is the VM interface that both the pre- and post-transition chains must
// implement.
type Chain interface {
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.SetPreferenceWithContextChainVM
	block.StateSyncableVM
}

// VM wraps a pre- and a post-transition [Chain], forwarding calls to whichever
// is currently active and switching from the pre- to the post-transition chain
// at the transition time.
type VM struct {
	// transition parameters
	preTransitionChain  Chain
	postTransitionChain Chain
	transitionTime      time.Time

	// chain parameters
	postChainCtx *snow.Context // Has modified [snow.Context.Lock] and [snow.Context.Metrics]
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

// current holds the active chain and its per-chain state. It is replaced on
// initialization and transition.
type current struct {
	chain    Chain
	requests *requests

	ctx       context.Context
	ctxCancel context.CancelFunc
}

var transitionedKey = prefixdb.MakePrefix([]byte("transitioned"))

func (vm *VM) Initialize(
	ctx context.Context,
	preChainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	gatherer := metrics.NewPrefixGatherer()
	if err := preChainCtx.Metrics.Register("transition", gatherer); err != nil {
		return err
	}

	// Give the post-transition chain its own gatherer to avoid
	// double-registering metrics, and copy the rest of the context so the
	// transition never races on the shared [snow.Context].
	vm.postChainCtx = &snow.Context{
		NetworkID:       preChainCtx.NetworkID,
		SubnetID:        preChainCtx.SubnetID,
		ChainID:         preChainCtx.ChainID,
		NodeID:          preChainCtx.NodeID,
		PublicKey:       preChainCtx.PublicKey,
		NetworkUpgrades: preChainCtx.NetworkUpgrades,
		XChainID:        preChainCtx.XChainID,
		CChainID:        preChainCtx.CChainID,
		AVAXAssetID:     preChainCtx.AVAXAssetID,
		Log:             preChainCtx.Log,
		// The lock is deprecated and unused by SAE, so this copy gets a fresh
		// one. Coreth still uses it and MUST get the original context, not this
		// copy.
		Lock:           sync.RWMutex{},
		SharedMemory:   preChainCtx.SharedMemory,
		BCLookup:       preChainCtx.BCLookup,
		Metrics:        gatherer,
		WarpSigner:     preChainCtx.WarpSigner,
		ValidatorState: preChainCtx.ValidatorState,
		ChainDataDir:   preChainCtx.ChainDataDir,
	}
	vm.db = db
	vm.genesisBytes = genesisBytes
	vm.upgradeBytes = upgradeBytes
	vm.configBytes = configBytes
	vm.fxs = fxs
	vm.appSender = appSender

	vm.connections = newConnections()
	vm.httpHandlers = newHTTPHandlers()

	log := preChainCtx.Log
	log.Info("checking for transition marker")
	has, err := vm.db.Has(transitionedKey)
	if err != nil {
		return fmt.Errorf("checking for transition marker: %w", err)
	}

	if has {
		log.Info("initializing post-transition VM")
		if err := vm.initChain(ctx, vm.postTransitionChain, vm.postChainCtx); err != nil {
			return fmt.Errorf("initializing post-transition VM: %w", err)
		}
		vm.transitioned = true
		return nil
	}

	log.Info("initializing pre-transition VM")
	if err := vm.initChain(ctx, vm.preTransitionChain, preChainCtx); err != nil {
		return fmt.Errorf("initializing pre-transition VM: %w", err)
	}

	// Genesis may already be past the transition time, or we may have crashed
	// after accepting the transition block but before marking it.
	lastAcceptedID, err := vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("loading last accepted ID: %w", err)
	}
	lastAccepted, err := vm.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return fmt.Errorf("loading last accepted block %s: %w", lastAcceptedID, err)
	}
	if lastAccepted.Timestamp().Before(vm.transitionTime) {
		return nil
	}
	return vm.transition(ctx, lastAccepted)
}

// transition switches from the pre- to the post-transition chain. The
// pre-transition chain must be active.
func (vm *VM) transition(ctx context.Context, last snowman.Block) error {
	// Cancel first so a blocked [VM.WaitForEvent] releases its read lock,
	// letting us take the write lock below.
	vm.current.ctxCancel()

	vm.transitionLock.Lock()
	defer vm.transitionLock.Unlock()

	lastID := last.ID()
	log := vm.postChainCtx.Log
	log.Info("transitioning VMs",
		zap.Stringer("blkID", lastID),
		zap.Uint64("height", last.Height()),
		zap.Time("timestamp", last.Timestamp()),
	)

	log.Info("shutting down pre-transition VM")
	if err := vm.preTransitionChain.Shutdown(ctx); err != nil {
		return fmt.Errorf("closing pre-transition chain: %w", err)
	}

	log.Info("writing transition marker")
	if err := vm.db.Put(transitionedKey, nil); err != nil {
		return fmt.Errorf("writing transition marker: %w", err)
	}

	log.Info("initializing post-transition VM")
	if err := vm.initChain(ctx, vm.postTransitionChain, vm.postChainCtx); err != nil {
		return fmt.Errorf("initializing post-transition VM: %w", err)
	}

	log.Info("setting post-transition VM state",
		zap.Stringer("state", vm.consensusState),
	)
	if vm.consensusState != snow.Initializing {
		if err := vm.postTransitionChain.SetState(ctx, vm.consensusState); err != nil {
			return fmt.Errorf("setting consensus state: %w", err)
		}
	}

	log.Info("copying connections to the post-transition VM")
	if err := vm.connections.reconnect(ctx, vm.postTransitionChain); err != nil {
		return fmt.Errorf("reconnecting to vm: %w", err)
	}

	log.Info("updating HTTP handlers to route to the post-transition VM")
	newHandlers, err := vm.postTransitionChain.CreateHandlers(ctx)
	if err != nil {
		return fmt.Errorf("creating http handlers: %w", err)
	}
	vm.httpHandlers.set(newHandlers)

	// The VM is only notified of preference changes, so if the consensus engine
	// previously set the preference, we must manually set the post-transition
	// VM's preference.
	//
	// Failing to due this could leave the preference uninitialized, which could
	// cause block building to error.
	log.Info("initializing post-transition VM preference",
		zap.Stringer("blkID", lastID),
	)
	if err := vm.postTransitionChain.SetPreference(ctx, lastID); err != nil {
		return fmt.Errorf("setting post-transition preference: %w", err)
	}

	vm.transitioned = true
	log.Info("transition finished successfully")
	return nil
}

// initChain configures VM to dispatch the provided chain with the given
// context.
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

	ctx, ctxCancel := context.WithCancel(context.Background())
	vm.current = &current{
		chain:     chain,
		requests:  &requests,
		ctx:       ctx,
		ctxCancel: ctxCancel,
	}
	return nil
}
