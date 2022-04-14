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
	// Note: we intentionally omit adding metrics for StateSyncEnabled

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

func (vm *blockVM) StateSyncParseSummary(summaryBytes []byte) (common.Summary, error) {
	if vm.ssVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}
	start := vm.clock.Time()
	summary, err := vm.ssVM.StateSyncParseSummary(summaryBytes)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.parseSummary.Observe(float64(end.Sub(start)))

	return summary, err
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

func (vm *blockVM) StateSyncGetOngoingSummary() (common.Summary, error) {
	if vm.ssVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}
	start := vm.clock.Time()
	summary, err := vm.ssVM.StateSyncGetOngoingSummary()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.getOngoingStateSyncSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) StateSyncGetResult() (ids.ID, uint64, error) {
	if vm.ssVM == nil {
		return ids.Empty, 0, common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	blkID, height, err := vm.ssVM.StateSyncGetResult()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.lastSummaryBlockID.Observe(float64(end.Sub(start)))

	return blkID, height, err
}

func (vm *blockVM) StateSyncSetLastSummaryBlock(blkBytes []byte) error {
	if vm.ssVM == nil {
		return common.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	err := vm.ssVM.StateSyncSetLastSummaryBlock(blkBytes)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.setLastSummaryBlockID.Observe(float64(end.Sub(start)))

	return err
}
