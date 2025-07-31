// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.ssVM == nil {
		return false, nil
	}

	start := vm.clock.Time()
	enabled, err := vm.ssVM.StateSyncEnabled(ctx)
	end := vm.clock.Time()
	vm.blockMetrics.stateSyncEnabled.Observe(float64(end.Sub(start)))
	return enabled, err
}

func (vm *blockVM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.GetOngoingSyncStateSummary(ctx)
	end := vm.clock.Time()
	vm.blockMetrics.getOngoingSyncStateSummary.Observe(float64(end.Sub(start)))
	return summary, err
}

func (vm *blockVM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.GetLastStateSummary(ctx)
	end := vm.clock.Time()
	vm.blockMetrics.getLastStateSummary.Observe(float64(end.Sub(start)))
	return summary, err
}

func (vm *blockVM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.ParseStateSummary(ctx, summaryBytes)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.blockMetrics.parseStateSummaryErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.parseStateSummary.Observe(duration)
	return summary, nil
}

func (vm *blockVM) GetStateSummary(ctx context.Context, height uint64) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	start := vm.clock.Time()
	summary, err := vm.ssVM.GetStateSummary(ctx, height)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.blockMetrics.getStateSummaryErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.getStateSummary.Observe(duration)
	return summary, nil
}
