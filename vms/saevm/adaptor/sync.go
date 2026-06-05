// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adaptor

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// SyncableVM is a [ChainVM] that also supports state sync. See
// [block.StateSyncableVM] and [block.StateSummary] for more documentation.
//
// Every method below is required: state sync is mandatory under this
// conversion. The summary getters MUST return database.ErrNotFound when no
// summary is available, never a nil SP.
type SyncableVM[BP BlockProperties, SP SummaryProperties] interface {
	ChainVM[BP]
	StateSync[SP]
}

// StateSync is the state-sync surface of a VM, independent of the block type.
type StateSync[SP SummaryProperties] interface {
	StateSyncEnabled(context.Context) (bool, error)
	GetLastStateSummary(context.Context) (SP, error)
	GetOngoingSyncStateSummary(context.Context) (SP, error)
	GetStateSummary(context.Context, uint64) (SP, error)
	ParseStateSummary(context.Context, []byte) (SP, error)

	// Transferred from [block.StateSummary]
	AcceptSummary(context.Context, SP) (block.StateSyncMode, error)
}

// SummaryProperties is a read-only subset of [block.StateSummary].
// [block.StateSummary.Accept] is not included, as it is handled by [SyncableVM].
type SummaryProperties interface {
	ID() ids.ID
	Bytes() []byte
	Height() uint64
}

// FullVM is the maximal interface for a snowman VM.
// It is the union of [ChainVMWithContext] and [block.StateSyncableVM].
type FullVM interface {
	ChainVMWithContext
	block.StateSyncableVM
}

// ConvertStateSync transforms a [SyncableVM] into a [FullVM].
func ConvertStateSync[BP BlockProperties, SP SummaryProperties](vm SyncableVM[BP, SP]) FullVM {
	return syncAdaptor[BP, SP]{
		ChainVMWithContext: Convert(vm),
		vm:                 vm,
	}
}

type syncAdaptor[BP BlockProperties, SP SummaryProperties] struct {
	ChainVMWithContext
	vm SyncableVM[BP, SP]
}

// Summary is an implementation of [block.StateSummary], used by chains returned
// by [ConvertStateSync]. The [SummaryProperties] can be accessed with
// [Summary.Unwrap].
//
// Summary holds the [StateSync] surface rather than the full [SyncableVM], so it
// depends only on SP and never on the block type. It mirrors how [Block] holds a
// [ChainVM].
type Summary[SP SummaryProperties] struct {
	s  SP
	vm StateSync[SP]
}

// Unwrap returns the underlying [SummaryProperties] of the [Summary].
func (s Summary[SP]) Unwrap() SP {
	return s.s
}

func (vm syncAdaptor[BP, SP]) newSummary(s SP, err error) (block.StateSummary, error) {
	if err != nil {
		return nil, err
	}
	return Summary[SP]{s, vm.vm}, nil
}

func (vm syncAdaptor[BP, SP]) StateSyncEnabled(ctx context.Context) (bool, error) {
	return vm.vm.StateSyncEnabled(ctx)
}

func (vm syncAdaptor[BP, SP]) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	return vm.newSummary(vm.vm.GetLastStateSummary(ctx))
}

func (vm syncAdaptor[BP, SP]) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	return vm.newSummary(vm.vm.GetOngoingSyncStateSummary(ctx))
}

func (vm syncAdaptor[BP, SP]) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	return vm.newSummary(vm.vm.GetStateSummary(ctx, summaryHeight))
}

func (vm syncAdaptor[BP, SP]) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return vm.newSummary(vm.vm.ParseStateSummary(ctx, summaryBytes))
}

// ID propagates the respective method from the [SummaryProperties] carried by s.
func (s Summary[_]) ID() ids.ID { return s.s.ID() }

// Bytes propagates the respective method from the [SummaryProperties] carried by s.
func (s Summary[_]) Bytes() []byte { return s.s.Bytes() }

// Height propagates the respective method from the [SummaryProperties] carried by s.
func (s Summary[_]) Height() uint64 { return s.s.Height() }

// Accept calls AcceptSummary(s) on the [SyncableVM] that created s.
func (s Summary[_]) Accept(ctx context.Context) (block.StateSyncMode, error) {
	return s.vm.AcceptSummary(ctx, s.s)
}
