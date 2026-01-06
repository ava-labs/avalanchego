// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	oteltrace "go.opentelemetry.io/otel/trace"
)

func (vm *blockVM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.ssVM == nil {
		return false, nil
	}

	ctx, span := vm.tracer.Start(ctx, vm.stateSyncEnabledTag)
	defer span.End()

	return vm.ssVM.StateSyncEnabled(ctx)
}

func (vm *blockVM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.getOngoingSyncStateSummaryTag)
	defer span.End()

	return vm.ssVM.GetOngoingSyncStateSummary(ctx)
}

func (vm *blockVM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.getLastStateSummaryTag)
	defer span.End()

	return vm.ssVM.GetLastStateSummary(ctx)
}

func (vm *blockVM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.parseStateSummaryTag, oteltrace.WithAttributes(
		attribute.Int("summaryLen", len(summaryBytes)),
	))
	defer span.End()

	return vm.ssVM.ParseStateSummary(ctx, summaryBytes)
}

func (vm *blockVM) GetStateSummary(ctx context.Context, height uint64) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.getStateSummaryTag, oteltrace.WithAttributes(
		attribute.Int64("height", int64(height)),
	))
	defer span.End()

	return vm.ssVM.GetStateSummary(ctx, height)
}
