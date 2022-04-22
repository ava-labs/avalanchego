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

func (ss *statefulSummary) Accept() (bool, error) {
	// A non-empty summary must update the block height index with its blockID
	// (i.e. the ID of the block summary refers to). This helps resuming
	// state sync after a shutdown since height index allows retrieving
	// proposerBlkID from innerSummary.Height.
	if ss.ID() != ids.Empty {
		if err := ss.vm.updateHeightIndex(ss.Height(), ss.BlockID()); err != nil {
			return false, err
		}

		if err := ss.vm.db.Commit(); err != nil {
			return false, err
		}
	}

	return ss.innerSummary.Accept()
}
