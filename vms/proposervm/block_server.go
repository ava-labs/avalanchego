package proposervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/indexes"
)

var _ indexes.BlockServer = &VM{}

// LastAcceptedWrappingBlkID implements BlockServer interface
func (vm *VM) LastAcceptedWrappingBlkID() (ids.ID, error) {
	return vm.LastAccepted()
}

// LastAcceptedInnerBlkID implements BlockServer interface
func (vm *VM) LastAcceptedInnerBlkID() (ids.ID, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	return vm.ChainVM.LastAccepted()
}

// GetWrappingBlk implements BlockServer interface
func (vm *VM) GetWrappingBlk(blkID ids.ID) (indexes.WrappingBlock, error) {
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

// DbCommit implements BlockServer interface
func (vm *VM) DBCommit() error {
	return vm.db.Commit()
}
