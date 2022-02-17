// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func (vm *blockVM) RegisterStateSyncer(stateSyncers []ids.ShortID) error {
	if vm.ssVM == nil {
		return common.ErrStateSyncableVMNotImplemented
	}

	return vm.ssVM.RegisterStateSyncer(stateSyncers)
}

func (vm *blockVM) StateSyncEnabled() (bool, error) {
	if vm.ssVM == nil {
		return false, common.ErrStateSyncableVMNotImplemented
	}

	return vm.ssVM.StateSyncEnabled()
}

func (vm *blockVM) StateSyncGetLastSummary() (common.Summary, error) {
	if vm.ssVM == nil {
		return common.Summary{}, common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.StateSyncGetLastSummary()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.lastSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) StateSyncIsSummaryAccepted(key []byte) (bool, error) {
	if vm.ssVM == nil {
		return false, common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	accepted, err := vm.ssVM.StateSyncIsSummaryAccepted(key)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.isSummaryAccepted.Observe(float64(end.Sub(start)))

	return accepted, err
}

func (vm *blockVM) StateSync(accepted []common.Summary) error {
	if vm.ssVM == nil {
		return common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	err := vm.ssVM.StateSync(accepted)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.syncState.Observe(float64(end.Sub(start)))

	return err
}

func (vm *blockVM) GetLastSummaryBlockID() (ids.ID, error) {
	if vm.ssVM == nil {
		return ids.Empty, common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	blkID, err := vm.ssVM.GetLastSummaryBlockID()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.lastSummaryBlockID.Observe(float64(end.Sub(start)))

	return blkID, err
}

func (vm *blockVM) SetLastSummaryBlock(blkBytes []byte) error {
	if vm.ssVM == nil {
		return common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	err := vm.ssVM.SetLastSummaryBlock(blkBytes)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.setLastSummaryBlockID.Observe(float64(end.Sub(start)))

	return err
}
