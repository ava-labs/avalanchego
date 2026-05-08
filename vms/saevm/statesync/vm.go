// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"net/http"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

type Config struct {
	sae.Config

	StateSyncEnabled bool
	StateSyncIDs     []ids.NodeID
}

type VM[T hook.Transaction] struct {
	sae.Network

	// Most operations of the VM are expected to not be called until after state
	// sync is complete. To avoid having to synchronize access to the VM before
	// state sync is complete, we use an atomic to swap out the inner VM once
	// state sync is complete for a minimal set of operations.
	atomicVM       utils.Atomic[*sae.VM]
	consensusState utils.Atomic[snow.State]

	snowCtx     *snow.Context
	chainConfig *params.ChainConfig
	cfg         Config
	hooks       hook.PointsG[T]
	db          ethdb.Database
	metrics     *prometheus.Registry

	stateSyncDone chan struct{}
}

func NewVM[T hook.Transaction](
	ctx context.Context,
	hooks hook.PointsG[T],
	cfg Config,
	snowCtx *snow.Context,
	chainConfig *params.ChainConfig,
	db ethdb.Database,
	lastSynchronous *types.Block,
	sender common.AppSender,
) (*VM[T], error) {
	metrics := prometheus.NewRegistry()
	network, err := sae.NewNetwork(snowCtx, sender, metrics)
	if err != nil {
		return nil, err
	}
	vm := &VM[T]{
		Network:       network,
		snowCtx:       snowCtx,
		chainConfig:   chainConfig,
		cfg:           cfg,
		hooks:         hooks,
		db:            db,
		metrics:       metrics,
		stateSyncDone: make(chan struct{}),
	}

	if !cfg.StateSyncEnabled {
		if err := vm.initInnerVM(ctx); err != nil {
			return nil, err
		}
	}

	return vm, nil
}

func (vm *VM[_]) initInnerVM(ctx context.Context) error {
	innerVM, err := sae.NewVM(
		ctx,
		vm.hooks,
		vm.cfg.Config,
		vm.snowCtx,
		vm.chainConfig,
		vm.db,
		nil, // lastSynchronous TODO
		vm.metrics,
		vm.Network,
	)
	if err != nil {
		return err
	}
	vm.atomicVM.Set(innerVM)
	vm.initSyncServer()
	return nil
}

// NewHTTPHandler implements [adaptor.SyncableVM].
func (vm *VM[_]) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	panic("unimplemented")
}

// CreateHandlers TODO
// Provide the handlers immediately after initialization, but they don't need
// to be available until after state sync is complete.
func (vm *VM[_]) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	panic("unimplemented")
}

// SetState implements [adaptor.SyncVM].
// Subtle: this method shadows the method (*VM).SetState of VM.VM.
func (vm *VM[_]) SetState(ctx context.Context, state snow.State) error {
	vm.consensusState.Set(state)
	if inner := vm.atomicVM.Get(); inner != nil {
		return inner.SetState(ctx, state)
	}
	return nil
}

// Shutdown implements [adaptor.SyncVM].
// Subtle: this method shadows the method (*VM).Shutdown of VM.VM.
func (vm *VM[_]) Shutdown(ctx context.Context) error {
	if inner := vm.atomicVM.Get(); inner != nil {
		return inner.Shutdown(ctx)
	}
	// TODO: cancel syncer
	return nil
}

// WaitForEvent implements [adaptor.SyncVM].
// Subtle: this method shadows the method (*VM).WaitForEvent of VM.VM.
func (vm *VM[_]) WaitForEvent(ctx context.Context) (common.Message, error) {
	if vm.consensusState.Get() == snow.NormalOp {
		return vm.atomicVM.Get().WaitForEvent(ctx)
	}

	select {
	case <-vm.stateSyncDone:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// AcceptBlock implements [adaptor.SyncableVM].
func (vm *VM[_]) AcceptBlock(ctx context.Context, block *blocks.Block) error {
	return vm.atomicVM.Get().AcceptBlock(ctx, block)
}

// BuildBlock implements [adaptor.SyncableVM].
func (vm *VM[_]) BuildBlock(ctx context.Context, blkCtx *block.Context) (*blocks.Block, error) {
	return vm.atomicVM.Get().BuildBlock(ctx, blkCtx)
}

// GetBlock implements [adaptor.SyncableVM].
func (vm *VM[_]) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	if inner := vm.atomicVM.Get(); inner != nil {
		return inner.GetBlock(ctx, id)
	}
	// TODO best effort get
	return nil, database.ErrNotFound
}

// GetBlockIDAtHeight implements [adaptor.SyncableVM].
func (vm *VM[_]) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if inner := vm.atomicVM.Get(); inner != nil {
		return inner.GetBlockIDAtHeight(ctx, height)
	}
	// TODO best effort get
	return ids.Empty, database.ErrNotFound
}

// HealthCheck implements [adaptor.SyncableVM].
func (vm *VM[_]) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}

// LastAccepted implements [adaptor.SyncableVM].
func (vm *VM[_]) LastAccepted(ctx context.Context) (ids.ID, error) {
	if inner := vm.atomicVM.Get(); inner != nil {
		return inner.LastAccepted(ctx)
	}
	// TODO best effort get
	return ids.Empty, database.ErrNotFound
}

// ParseBlock implements [adaptor.SyncableVM].
func (vm *VM[_]) ParseBlock(ctx context.Context, bytes []byte) (*blocks.Block, error) {
	if inner := vm.atomicVM.Get(); inner != nil {
		return inner.ParseBlock(ctx, bytes)
	}
	// TODO best effort parse
	return nil, database.ErrNotFound
}

// RejectBlock implements [adaptor.SyncableVM].
func (vm *VM[_]) RejectBlock(ctx context.Context, block *blocks.Block) error {
	return vm.atomicVM.Get().RejectBlock(ctx, block)
}

// SetPreference implements [adaptor.SyncableVM].
func (vm *VM[_]) SetPreference(ctx context.Context, id ids.ID, blkCtx *block.Context) error {
	return vm.atomicVM.Get().SetPreference(ctx, id, blkCtx)
}

// VerifyBlock implements [adaptor.SyncableVM].
func (vm *VM[_]) VerifyBlock(ctx context.Context, blkCtx *block.Context, block *blocks.Block) error {
	return vm.atomicVM.Get().VerifyBlock(ctx, blkCtx, block)
}

// Version implements [adaptor.SyncableVM].
func (vm *VM[_]) Version(ctx context.Context) (string, error) {
	return vm.atomicVM.Get().Version(ctx) // nil is fine, technically stateless
}
