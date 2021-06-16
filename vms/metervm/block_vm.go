// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/timer"
)

var _ block.ChainVM = &BlockVM{}

func NewBlockVM(vm block.ChainVM) block.ChainVM {
	return &BlockVM{
		ChainVM: vm,
	}
}

type BlockVM struct {
	block.ChainVM
	blockMetrics
	clock timer.Clock
}

func (vm *BlockVM) Initialize(
	ctx *snow.Context,
	db manager.Manager,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	if err := vm.blockMetrics.Initialize(fmt.Sprintf("metervm_%s", ctx.Namespace), ctx.Metrics); err != nil {
		return err
	}

	return vm.ChainVM.Initialize(ctx, db, genesisBytes, upgradeBytes, configBytes, toEngine, fxs)
}

func (vm *BlockVM) BuildBlock() (snowman.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.BuildBlock()
	end := vm.clock.Time()
	vm.blockMetrics.buildBlock.Observe(float64(end.Sub(start)))
	return blk, err
}

func (vm *BlockVM) ParseBlock(b []byte) (snowman.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.ParseBlock(b)
	end := vm.clock.Time()
	vm.blockMetrics.parseBlock.Observe(float64(end.Sub(start)))
	return blk, err
}

func (vm *BlockVM) GetBlock(id ids.ID) (snowman.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.GetBlock(id)
	end := vm.clock.Time()
	vm.blockMetrics.getBlock.Observe(float64(end.Sub(start)))
	return blk, err
}

func (vm *BlockVM) SetPreference(id ids.ID) error {
	start := vm.clock.Time()
	err := vm.ChainVM.SetPreference(id)
	end := vm.clock.Time()
	vm.blockMetrics.setPreference.Observe(float64(end.Sub(start)))
	return err
}

func (vm *BlockVM) LastAccepted() (ids.ID, error) {
	start := vm.clock.Time()
	lastAcceptedID, err := vm.ChainVM.LastAccepted()
	end := vm.clock.Time()
	vm.blockMetrics.lastAccepted.Observe(float64(end.Sub(start)))
	return lastAcceptedID, err
}
