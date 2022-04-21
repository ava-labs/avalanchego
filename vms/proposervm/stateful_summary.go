// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
)

var _ block.Summary = &statefulSummary{}

type statefulSummary struct {
	summary.ProposerSummaryIntf

	// stateful inner summary, retrieved via Parse
	innerSummary block.Summary

	vm *VM
}

func (ss *statefulSummary) Accept() error {
	// Following state sync introduction, we update height -> blockID index
	// with summary content in order to support resuming state sync in case
	// of shutdown. The height index allows to retrieve the proposerBlkID
	// of any state sync passed down to coreVM, so that the proposerVM state summary
	// information of any coreVM summary can be rebuilt and pass to the engine, even
	// following a shutdown.
	// Note that we won't download all the blocks associated with state summaries,
	// so proposerVM may not not all the full blocks indexed into height index. Same
	// is true for coreVM.
	// Finally note that an empty summary may be accepted, which has empty ID.
	// Such summary must not be added to height index.
	if ss.ID() != ids.Empty {
		if err := ss.vm.updateHeightIndex(ss.Height(), ss.ProposerBlockID()); err != nil {
			return err
		}

		if err := ss.vm.db.Commit(); err != nil {
			return nil
		}
	}

	return ss.innerSummary.Accept()
}
