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
	vm *VM

	// stateful inner summary, retrieved via Parse
	innerSummary block.Summary

	// block associated with the summary
	proposerBlock Block
}

func (ss *statefulSummary) Accept() (bool, error) {
	// A non-empty summary must update the block height index with its blockID
	// (i.e. the ID of the block summary refers to). This helps resuming
	// state sync after a shutdown since height index allows retrieving
	// proposerBlkID from innerSummary.Height.
	if ss.ID() != ids.Empty {
		// Store the block
		if postForkBlk, ok := ss.proposerBlock.(PostForkBlock); ok {
			if err := ss.vm.storePostForkBlock(postForkBlk); err != nil {
				return false, err
			}
		}

		ss.vm.syncSummary = ss
	}

	return ss.innerSummary.Accept()
}
