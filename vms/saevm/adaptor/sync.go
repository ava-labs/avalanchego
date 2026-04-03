// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adaptor

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type FullVM interface {
	ChainVMWithContext
	block.StateSyncableVM
}

type SyncVM[BP BlockProperties, SP SummaryProperties] interface {
	ChainVM[BP]

	StateSyncEnabled(context.Context) (bool, error)
	AcceptSummary(context.Context, SP) (block.StateSyncMode, error)
	GetLastStateSummary(context.Context) (SP, error)
	GetOngoingSyncStateSummary(context.Context) (SP, error)
	GetStateSummary(context.Context, uint64) (SP, error)
	ParseStateSummary(context.Context, []byte) (SP, error)
}

type SummaryProperties interface {
	ID() ids.ID
	Bytes() []byte
	Height() uint64
}

type syncAdaptor[BP BlockProperties, SP SummaryProperties] struct {
	ChainVMWithContext
	vm SyncVM[BP, SP]
}

func ConvertStateSync[BP BlockProperties, SP SummaryProperties](vm SyncVM[BP, SP]) FullVM {
	return syncAdaptor[BP, SP]{
		ChainVMWithContext: Convert(vm),
		vm:                 vm,
	}
}

type Summary[BP BlockProperties, SP SummaryProperties] struct {
	s  SP
	vm SyncVM[BP, SP]
}

func (vm syncAdaptor[BP, SP]) newSummary(s SP, err error) (block.StateSummary, error) {
	if err != nil {
		return nil, err
	}
	return Summary[BP, SP]{s, vm.vm}, nil
}

func (vm syncAdaptor[BP, SP]) StateSyncEnabled(ctx context.Context) (bool, error) {
	return vm.vm.StateSyncEnabled(ctx)
}

func (vm syncAdaptor[BP, SP]) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	return vm.newSummary(vm.vm.GetLastStateSummary(ctx))
}

func (vm syncAdaptor[BP, SP]) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	return vm.newSummary((vm.vm.GetOngoingSyncStateSummary(ctx)))
}

func (vm syncAdaptor[BP, SP]) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	return vm.newSummary(vm.vm.GetStateSummary(ctx, summaryHeight))
}

func (vm syncAdaptor[BP, SP]) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return vm.newSummary(vm.vm.ParseStateSummary(ctx, summaryBytes))
}

func (s Summary[_, _]) ID() ids.ID { return s.s.ID() }

func (s Summary[_, _]) Bytes() []byte { return s.s.Bytes() }

func (s Summary[_, _]) Height() uint64 { return s.s.Height() }

func (s Summary[_, _]) Accept(ctx context.Context) (block.StateSyncMode, error) {
	return s.vm.AcceptSummary(ctx, s.s)
}
