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

	// ShouldAcceptSummary reports whether a sync to the summary should
	// start. It runs under the chain's context lock inside
	// [block.StateSummary.Accept] and MUST be cheap: disk reads only, no
	// network, no side effects.
	ShouldAcceptSummary(context.Context, SP) (bool, error)

	// Sync blocks until all state for the summary is fetched and finalized.
	// A nil return means the VM is runnable.
	Sync(context.Context, SP) error
}

// SummaryProperties is a read-only subset of [block.StateSummary].
// [block.StateSummary.Accept] is not included, as it is handled by [Summary].
type SummaryProperties interface {
	ID() ids.ID
	Bytes() []byte
	Height() uint64
}

// ConvertStateSync transforms a [SyncableVM] into a [block.StateSyncableVM].
// The runner is the async boundary: [Summary.Accept] starts the VM's
// synchronous [SyncableVM.Sync] on it, and the VM reads completion
// ([Runner.WaitForEvent]) and the sync error ([Runner.Err]) from the same
// instance.
func ConvertStateSync[SP SummaryProperties](vm SyncableVM[SP], runner *Runner) block.StateSyncableVM {
	return syncAdaptor[SP]{
		SyncableVM: vm,
		runner:     runner,
	}
}

type syncAdaptor[SP SummaryProperties] struct {
	SyncableVM[SP]
	runner *Runner
}

// Summary is an implementation of [block.StateSummary], used by chains
// returned by [ConvertStateSync]. The [SummaryProperties] can be accessed
// with [Summary.Unwrap].
type Summary[SP SummaryProperties] struct {
	s      SP
	vm     SyncableVM[SP]
	runner *Runner
}

// Unwrap returns the underlying [SummaryProperties] of the [Summary].
func (s Summary[SP]) Unwrap() SP {
	return s.s
}

func (vm syncAdaptor[SP]) newSummary(s SP, err error) (block.StateSummary, error) {
	if err != nil {
		return nil, err
	}
	return Summary[SP]{s, vm.SyncableVM, vm.runner}, nil
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

// Accept asks the [SyncableVM] whether to sync to s and, if so, starts the
// VM's Sync on the runner. It returns [block.StateSyncStatic] iff the sync
// started; completion is signaled by [Runner.WaitForEvent].
func (s Summary[SP]) Accept(ctx context.Context) (block.StateSyncMode, error) {
	should, err := s.vm.ShouldAcceptSummary(ctx, s.s)
	if err != nil || !should {
		return block.StateSyncSkipped, err
	}
	if !s.runner.Start(func(ctx context.Context) error {
		return s.vm.Sync(ctx, s.s)
	}) {
		return block.StateSyncSkipped, nil
	}
	return block.StateSyncStatic, nil
}
