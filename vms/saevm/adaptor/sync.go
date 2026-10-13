// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adaptor

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// SyncableVM adapts a [block.StateSyncableVM] and [block.StateSummary] for
// // stateless use of the summary. See the respective interfaces for more details.
type SyncableVM[SP SummaryProperties] interface {
	StateSyncEnabled(context.Context) (bool, error)
	GetLastStateSummary(context.Context) (SP, error)
	GetOngoingSyncStateSummary(context.Context) (SP, error)
	GetStateSummary(context.Context, uint64) (SP, error)
	ParseStateSummary(context.Context, []byte) (SP, error)
	AcceptSummary(context.Context, SP) (block.StateSyncMode, error)
}

// SummaryProperties is a read-only subset of [block.StateSummary].
// [block.StateSummary.Accept] is not included, as it is handled by [SyncableVM].
type SummaryProperties interface {
	ID() ids.ID
	Bytes() []byte
	Height() uint64
}

// ConvertStateSync transforms a [SyncableVM] into a [block.StateSyncableVM].
func ConvertStateSync[SP SummaryProperties](vm SyncableVM[SP]) block.StateSyncableVM {
	return syncAdaptor[SP]{
		SyncableVM: vm,
	}
}

type syncAdaptor[SP SummaryProperties] struct {
	SyncableVM[SP]
}

// Summary is an implementation of [block.StateSummary], used by chains returned
// by [ConvertStateSync]. The [SummaryProperties] can be accessed with
// [Summary.Unwrap].
type Summary[SP SummaryProperties] struct {
	s  SP
	vm SyncableVM[SP]
}

// Unwrap returns the underlying [SummaryProperties] of the [Summary].
func (s Summary[SP]) Unwrap() SP {
	return s.s
}

func (vm syncAdaptor[SP]) newSummary(s SP, err error) (block.StateSummary, error) {
	if err != nil {
		return nil, err
	}
	return Summary[SP]{s, vm.SyncableVM}, nil
}

func (vm syncAdaptor[SP]) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	return vm.newSummary(vm.SyncableVM.GetLastStateSummary(ctx))
}

func (vm syncAdaptor[SP]) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	return vm.newSummary(vm.SyncableVM.GetOngoingSyncStateSummary(ctx))
}

func (vm syncAdaptor[SP]) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	return vm.newSummary(vm.SyncableVM.GetStateSummary(ctx, summaryHeight))
}

func (vm syncAdaptor[SP]) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return vm.newSummary(vm.SyncableVM.ParseStateSummary(ctx, summaryBytes))
}

// ID propagates the respective method from the [SummaryProperties] carried by s.
func (s Summary[SP]) ID() ids.ID { return s.s.ID() }

// Bytes propagates the respective method from the [SummaryProperties] carried by s.
func (s Summary[SP]) Bytes() []byte { return s.s.Bytes() }

// Height propagates the respective method from the [SummaryProperties] carried by s.
func (s Summary[SP]) Height() uint64 { return s.s.Height() }

// Accept calls AcceptSummary(s) on the [SyncableVM] that created s.
func (s Summary[SP]) Accept(ctx context.Context) (block.StateSyncMode, error) {
	return s.vm.AcceptSummary(ctx, s.s)
}
