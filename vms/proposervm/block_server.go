// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/indexer"
)

var _ indexer.BlockServer = &VM{}

// LastAcceptedWrappingBlkID implements BlockServer interface
func (vm *VM) LastAcceptedWrappingBlkID() (ids.ID, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	return vm.State.GetLastAccepted()
}

// LastAcceptedInnerBlkID implements BlockServer interface
func (vm *VM) LastAcceptedInnerBlkID() (ids.ID, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	return vm.ChainVM.LastAccepted()
}

// GetWrappingBlk implements BlockServer interface
func (vm *VM) GetWrappingBlk(blkID ids.ID) (indexer.WrappingBlock, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	return vm.getPostForkBlock(blkID)
}

// GetInnerBlk implements BlockServer interface
func (vm *VM) GetInnerBlk(id ids.ID) (snowman.Block, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	return vm.ChainVM.GetBlock(id)
}
