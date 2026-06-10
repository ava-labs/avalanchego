// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

// VM is a wrapper around [sae.VM] that implements all of [adaptor.SyncableVM],
// except Initialize, which needs to be done by the harness.
type VM[S adaptor.SummaryProperties] struct {
	sae.Network
	adaptor.StateSyncable[S]

	lazyChain chainInitializer
	initOnce  sync.Once
	chain     utils.Atomic[adaptor.Chain[*blocks.Block]]
	state     utils.Atomic[snow.State]
}

type chainInitializer func() (adaptor.Chain[*blocks.Block], error)

func NewVM[S adaptor.SummaryProperties](
	network sae.Network,
	syncable adaptor.StateSyncable[S],
	lazyVM chainInitializer,
) (*VM[S], error) {
	return &VM[S]{
		Network:       network,
		StateSyncable: syncable,
		lazyChain:     lazyVM,
	}, nil
}

// Shutdown implements [adaptor.SyncableVM].
func (vm *VM[S]) Shutdown(ctx context.Context) error {
	if v := vm.chain.Get(); v != nil {
		return v.Shutdown(ctx)
	}
	// TODO shutdown syncer
	return nil
}

// SetState implements [adaptor.SyncableVM].
func (vm *VM[S]) SetState(ctx context.Context, state snow.State) error {
	var err error
	prev := vm.state.Swap(state)
	if prev < snow.Bootstrapping {
		vm.initOnce.Do(func() {
			var chain adaptor.Chain[*blocks.Block]
			chain, err = vm.lazyChain()
			if err != nil {
				return
			}
			vm.chain.Set(chain)
		})
	}
	return err
}

// WaitForEvent implements [adaptor.SyncableVM].
func (vm *VM[S]) WaitForEvent(ctx context.Context) (common.Message, error) {
	switch vm.state.Get() {
	case snow.Initializing, snow.StateSyncing:
		return vm.StateSyncable.WaitForEvent(ctx)
	case snow.Bootstrapping, snow.NormalOp:
		return vm.chain.Get().WaitForEvent(ctx)
	default:
		return 0, errors.New("unexpected state")
	}
}

// CreateHandlers implements [adaptor.SyncableVM].
func (vm *VM[S]) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	// TODO: something similar to transitionvm
	panic("unimplemented")
}

// GetBlockIDAtHeight implements [adaptor.SyncableVM].
func (vm *VM[S]) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm := vm.chain.Get(); vm != nil {
		return vm.GetBlockIDAtHeight(ctx, height)
	}

	// TODO add to syncable implementation
	return ids.ID{}, errors.New("unimplemented")
}

// LastAccepted implements [adaptor.SyncableVM].
func (vm *VM[S]) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm := vm.chain.Get(); vm != nil {
		return vm.LastAccepted(ctx)
	}

	// TODO add to syncable implementation
	return ids.ID{}, errors.New("unimplemented")
}

// AcceptBlock implements [adaptor.SyncableVM].
func (vm *VM[S]) AcceptBlock(ctx context.Context, blk *blocks.Block) error {
	return vm.chain.Get().AcceptBlock(ctx, blk)
}

// BuildBlock implements [adaptor.SyncableVM].
func (vm *VM[S]) BuildBlock(ctx context.Context, bCtx *block.Context) (*blocks.Block, error) {
	return vm.chain.Get().BuildBlock(ctx, bCtx)
}

// GetBlock implements [adaptor.SyncableVM].
func (vm *VM[S]) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	return vm.chain.Get().GetBlock(ctx, id)
}

// ParseBlock implements [adaptor.SyncableVM].
func (vm *VM[S]) ParseBlock(ctx context.Context, b []byte) (*blocks.Block, error) {
	return vm.chain.Get().ParseBlock(ctx, b)
}

// RejectBlock implements [adaptor.SyncableVM].
func (vm *VM[S]) RejectBlock(ctx context.Context, blk *blocks.Block) error {
	return vm.chain.Get().RejectBlock(ctx, blk)
}

// SetPreference implements [adaptor.SyncableVM].
func (vm *VM[S]) SetPreference(ctx context.Context, id ids.ID, bCtx *block.Context) error {
	return vm.chain.Get().SetPreference(ctx, id, bCtx)
}

// VerifyBlock implements [adaptor.SyncableVM].
func (vm *VM[S]) VerifyBlock(ctx context.Context, bCtx *block.Context, blk *blocks.Block) error {
	return vm.chain.Get().VerifyBlock(ctx, bCtx, blk)
}

func (vm *VM[S]) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}

// NewHTTPHandler implements [adaptor.SyncableVM].
func (vm *VM[S]) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, nil
}

// Version implements [adaptor.SyncableVM].
func (vm *VM[S]) Version(context.Context) (string, error) {
	return "", nil
}
