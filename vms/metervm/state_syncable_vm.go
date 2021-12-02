// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package metervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) StateSyncEnabled() (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSyncEnabled()
}

func (vm *blockVM) StateSyncGetLastSummary() (block.Summary, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.Summary{}, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := fsVM.StateSyncGetLastSummary()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.lastSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) StateSyncIsSummaryAccepted(key []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	accepted, err := fsVM.StateSyncIsSummaryAccepted(key)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.isSummaryAccepted.Observe(float64(end.Sub(start)))

	return accepted, err
}

func (vm *blockVM) StateSync(accepted []block.Summary) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	err := fsVM.StateSync(accepted)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.syncState.Observe(float64(end.Sub(start)))

	return err
}

func (vm *blockVM) GetLastSummaryBlockID() (ids.ID, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return ids.Empty, block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.GetLastSummaryBlockID()
}
