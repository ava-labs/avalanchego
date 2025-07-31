// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
)

var _ block.StateSummary = (*stateSummary)(nil)

// stateSummary implements block.StateSummary by layering three objects:
//
//  1. [statelessSummary] carries all summary marshallable content along with
//     data immediately retrievable from it.
//  2. [innerSummary] reports the height of the summary as well as notifying the
//     inner vm of the summary's acceptance.
//  3. [block] is used to update the proposervm's last accepted block upon
//     Accept.
//
// Note: summary.StatelessSummary contains the data to build both [innerSummary]
// and [block].
type stateSummary struct {
	summary.StateSummary

	// inner summary, retrieved via Parse
	innerSummary block.StateSummary

	// block associated with the summary
	block PostForkBlock

	vm *VM
}

func (s *stateSummary) Height() uint64 {
	return s.innerSummary.Height()
}

func (s *stateSummary) Accept(ctx context.Context) (block.StateSyncMode, error) {
	// set fork height first, before accepting proposerVM full block
	// which updates height index (among other indices)
	if err := s.vm.State.SetForkHeight(s.StateSummary.ForkHeight()); err != nil {
		return block.StateSyncSkipped, err
	}

	// Mark the summary as accepted on the outerVM iff it rolls forward.
	// We refuse to roll the proposerVM backward because it violates the invariant
	// that the proposerVM index is always >= the innerVM index.
	if s.vm.lastAcceptedHeight < s.Height() {
		// We store the full proposerVM block associated with the summary
		// and update height index with it, so that state sync could resume
		// after a shutdown.
		if err := s.block.acceptOuterBlk(); err != nil {
			return block.StateSyncSkipped, err
		}
	}

	// innerSummary.Accept may fail with the proposerVM block and index already
	// updated. The error would be treated as fatal and the chain would then be
	// repaired upon the VM restart.
	// After the inner summary is accepted, the engine transitions to bootstrapping
	// and SetState is responsible for re-aligning the ProposerVM to the height reported
	// by the inner VM after handling state sync.
	return s.innerSummary.Accept(ctx)
}
