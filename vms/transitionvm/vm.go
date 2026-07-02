// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package transitionvm implements a [smblock.ChainVM] that switches from one
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
	"github.com/ava-labs/avalanchego/utils"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ Chain = (*VM)(nil)

// Chain is the VM interface that both the pre- and post-transition chains must
// implement.
type Chain interface {
	smblock.ChainVM
	smblock.BuildBlockWithContextChainVM
	smblock.SetPreferenceWithContextChainVM
	smblock.StateSyncableVM
}

// VM wraps a pre- and a post-transition [Chain], forwarding calls to whichever
// is currently active and switching from the pre- to the post-transition chain
// at the transition time.
type VM struct {
	// transition parameters
	preTransitionChain  Chain
	postTransitionChain Chain
	transitionTime      time.Time
	drainTimeout        time.Duration

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
	consensusState utils.Atomic[snow.State]
	setPreference  utils.Atomic[bool]
	connections    *connections
	httpHandlers   *httpHandlers
	current        *current
}

// current holds the active chain and its per-chain state. It is replaced on
// initialization and transition.
type current struct {
	chain    Chain
	chainCtx *snow.Context
	requests *requests

	ctx       context.Context
	ctxCancel context.CancelFunc
}

var transitionedKey = prefixdb.MakePrefix([]byte("transitioned"))

func (vm *VM) Initialize(
	ctx context.Context,
	engineChainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	preChainCtx := copyContext(engineChainCtx)

	// Give the post-transition chain its own gatherer to avoid
	// double-registering metrics, and copy the rest of the context so the
	// transition never races on the shared [snow.Context].
	vm.postChainCtx = copyContext(engineChainCtx)
	vm.postChainCtx.Metrics = metrics.NewPrefixGatherer()
	if err := preChainCtx.Metrics.Register("transition", vm.postChainCtx.Metrics); err != nil {
		return err
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

// copyContext returns a new context with all the same fields except for
// [snow.Context.Lock], which can not be copied.
func copyContext(ctx *snow.Context) *snow.Context {
	return &snow.Context{
		NetworkID:       ctx.NetworkID,
		SubnetID:        ctx.SubnetID,
		ChainID:         ctx.ChainID,
		NodeID:          ctx.NodeID,
		PublicKey:       ctx.PublicKey,
		NetworkUpgrades: ctx.NetworkUpgrades,
		XChainID:        ctx.XChainID,
		CChainID:        ctx.CChainID,
		AVAXAssetID:     ctx.AVAXAssetID,
		Log:             ctx.Log,
		Lock:            sync.RWMutex{}, // The lock is not copied
		SharedMemory:    ctx.SharedMemory,
		BCLookup:        ctx.BCLookup,
		Metrics:         ctx.Metrics,
		WarpSigner:      ctx.WarpSigner,
		ValidatorState:  ctx.ValidatorState,
		ChainDataDir:    ctx.ChainDataDir,
	}
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

	// API requests are queued and drained during the transition to prevent APIs
	// from hitting the pre-transition VM while it is shutting down.
	log.Info("blocking API requests")
	vm.httpHandlers.block()
	defer func() {
		log.Info("unblocking API requests")
		vm.httpHandlers.unblock()
	}()

	// Draining the in-flight API requests blocks, but does not block forever.
	// Websockets are long-lived connections which are not able to be gracefully
	// terminated during the transition. This means that websocket connections
	// can (and will) cause this to timeout during the transition.
	log.Info("draining in-flight API requests to the pre-transition VM",
		zap.Duration("timeout", vm.drainTimeout),
	)
	drainCtx, cancelDrain := context.WithTimeout(ctx, vm.drainTimeout)
	defer cancelDrain()
	if err := vm.httpHandlers.drain(drainCtx); err != nil {
		log.Warn("abandoning API requests still in flight after drain timeout",
			zap.String("stack", utils.GetStacktrace(true)),
			zap.Error(err),
		)
	}

	log.Info("shutting down pre-transition VM")
	vm.current.chainCtx.Lock.Lock()
	err := vm.preTransitionChain.Shutdown(ctx)
	vm.current.chainCtx.Lock.Unlock()
	if err != nil {
		return fmt.Errorf("closing pre-transition chain: %w", err)
	}

	// Since shutdown finished without error for the pre-transition VM, we
	// expect all DB writes to have been flushed. So it is now safe to write the
	// transition marker. The transition marker MUST be written before
	// initializing the post-transition VM, because the post-transition VM may
	// perform disk operations that are incompatible with the pre-transition VM.
	log.Info("writing transition marker")
	if err := vm.db.Put(transitionedKey, nil); err != nil {
		return fmt.Errorf("writing transition marker: %w", err)
	}

	log.Info("initializing post-transition VM")
	if err := vm.initChain(ctx, vm.postTransitionChain, vm.postChainCtx); err != nil {
		return fmt.Errorf("initializing post-transition VM: %w", err)
	}

	vm.postChainCtx.Lock.Lock()
	defer vm.postChainCtx.Lock.Unlock()

	if state := vm.consensusState.Get(); state != snow.Initializing {
		log.Info("setting post-transition VM state",
			zap.Stringer("state", state),
		)
		if err := vm.postTransitionChain.SetState(ctx, state); err != nil {
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

	if vm.setPreference.Get() {
		// The VM is only notified of preference changes, so if the consensus
		// engine previously set the preference, we must manually set the
		// post-transition VM's preference.
		//
		// Failing to do this could leave the preference uninitialized, which
		// could cause block building to error.
		log.Info("initializing post-transition VM preference",
			zap.Stringer("blkID", lastID),
		)
		if err := vm.postTransitionChain.SetPreference(ctx, lastID); err != nil {
			return fmt.Errorf("setting post-transition preference: %w", err)
		}
	}

	vm.transitioned = true
	log.Info("transition finished successfully")
	return nil
}

// initChain configures VM to dispatch the provided chain with the given
// context.
func (vm *VM) initChain(ctx context.Context, chain Chain, chainCtx *snow.Context) error {
	chainCtx.Lock.Lock()
	defer chainCtx.Lock.Unlock()

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
		chainCtx:  chainCtx,
		requests:  &requests,
		ctx:       ctx,
		ctxCancel: ctxCancel,
	}
	return nil
}
