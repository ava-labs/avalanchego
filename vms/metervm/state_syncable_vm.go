// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func (vm *blockVM) StateSyncEnabled() (bool, error) {
	if vm.ssVM == nil {
		return false, common.ErrStateSyncableVMNotImplemented
	}

	return vm.ssVM.StateSyncEnabled()
}

func (vm *blockVM) StateSyncGetLastSummary() (common.Summary, error) {
	if vm.ssVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.StateSyncGetLastSummary()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.lastSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) ParseSummary(summaryBytes []byte) (common.Summary, error) {
	if vm.ssVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	return vm.ssVM.ParseSummary(summaryBytes)
}

func (vm *blockVM) StateSyncGetSummary(key uint64) (common.Summary, error) {
	if vm.ssVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.StateSyncGetSummary(key)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.isSummaryAccepted.Observe(float64(end.Sub(start)))

	return summary, err
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

func (vm *blockVM) GetOngoingStateSyncSummary() (common.Summary, error) {
	if vm.ssVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	return vm.ssVM.GetOngoingStateSyncSummary()
}

func (vm *blockVM) GetStateSyncResult() (ids.ID, uint64, error) {
	if vm.ssVM == nil {
		return ids.Empty, 0, common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	blkID, height, err := vm.ssVM.GetStateSyncResult()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.lastSummaryBlockID.Observe(float64(end.Sub(start)))

	return blkID, height, err
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
