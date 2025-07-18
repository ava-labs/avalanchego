// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ vertex.LinearizableVM = (*initializeOnLinearizeVM)(nil)
	_ block.ChainVM         = (*linearizeOnInitializeVM)(nil)
)

// initializeOnLinearizeVM transforms the consensus engine's call to Linearize
// into a call to Initialize. This enables the proposervm to be initialized by
// the call to Linearize. This also provides the stopVertexID to the
// linearizeOnInitializeVM.
type initializeOnLinearizeVM struct {
	vertex.DAGVM
	vmToInitialize common.VM
	vmToLinearize  *linearizeOnInitializeVM

	ctx              *snow.Context
	db               database.Database
	genesisBytes     []byte
	upgradeBytes     []byte
	configBytes      []byte
	fxs              []*common.Fx
	appSender        common.AppSender
	waitForLinearize chan struct{}
	linearizeOnce    sync.Once
}

func (vm *initializeOnLinearizeVM) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-vm.waitForLinearize:
		return vm.vmToInitialize.WaitForEvent(ctx)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (vm *initializeOnLinearizeVM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	vm.vmToLinearize.stopVertexID = stopVertexID
	defer vm.linearizeOnce.Do(func() {
		close(vm.waitForLinearize)
	})
	return vm.vmToInitialize.Initialize(
		ctx,
		vm.ctx,
		vm.db,
		vm.genesisBytes,
		vm.upgradeBytes,
		vm.configBytes,
		vm.fxs,
		vm.appSender,
	)
}

// linearizeOnInitializeVM transforms the proposervm's call to Initialize into a
// call to Linearize.
type linearizeOnInitializeVM struct {
	vertex.LinearizableVMWithEngine
	stopVertexID ids.ID
}

func NewLinearizeOnInitializeVM(vm vertex.LinearizableVMWithEngine) *linearizeOnInitializeVM {
	return &linearizeOnInitializeVM{
		LinearizableVMWithEngine: vm,
	}
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	_ *snow.Context,
	_ database.Database,
	_ []byte,
	_ []byte,
	_ []byte,
	_ []*common.Fx,
	_ common.AppSender,
) error {
	return vm.Linearize(ctx, vm.stopVertexID)
}
