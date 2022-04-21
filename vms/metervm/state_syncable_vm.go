// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) StateSyncEnabled() (bool, error) {
	if vm.ssVM == nil {
		return false, nil
	}

	start := vm.clock.Time()
	enabled, err := vm.ssVM.StateSyncEnabled()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.getLastStateSummary.Observe(float64(end.Sub(start)))
	return enabled, err
}

func (vm *blockVM) GetOngoingSyncStateSummary() (block.Summary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}
	start := vm.clock.Time()
	summary, err := vm.ssVM.GetOngoingSyncStateSummary()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.GetOngoingSyncStateSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) GetLastStateSummary() (block.Summary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.GetLastStateSummary()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.getLastStateSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) ParseStateSummary(summaryBytes []byte) (block.Summary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}
	start := vm.clock.Time()
	summary, err := vm.ssVM.ParseStateSummary(summaryBytes)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.parseStateSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) GetStateSummary(height uint64) (block.Summary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.GetStateSummary(height)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.getStateSummary.Observe(float64(end.Sub(start)))

	return summary, err
}

func (vm *blockVM) GetStateSyncResult() (ids.ID, uint64, error) {
	if vm.ssVM == nil {
		return ids.Empty, 0, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	blkID, height, err := vm.ssVM.GetStateSyncResult()
	end := vm.clock.Time()
	vm.stateSummaryMetrics.getStateSyncResult.Observe(float64(end.Sub(start)))

	return blkID, height, err
}

func (vm *blockVM) ParseStateSyncableBlock(blkBytes []byte) (snowman.StateSyncableBlock, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	stateSyncableBlk, err := vm.ssVM.ParseStateSyncableBlock(blkBytes)
	end := vm.clock.Time()
	vm.stateSummaryMetrics.parseStateSyncableBlock.Observe(float64(end.Sub(start)))

	return stateSyncableBlk, err
}
