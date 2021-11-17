// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package proposervm

import "github.com/ava-labs/avalanchego/snow/engine/snowman/block"

func (vm *VM) StateSyncEnabled() (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSyncEnabled()
}

func (vm *VM) StateSyncGetLastSummary() ([]byte, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSyncGetLastSummary()
}

func (vm *VM) StateSyncIsSummaryAccepted(summary []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSyncIsSummaryAccepted(summary)
}

func (vm *VM) StateSync(accepted [][]byte) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSync(accepted)
}
