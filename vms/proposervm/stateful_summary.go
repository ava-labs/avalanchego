// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
)

var _ block.Summary = &statefulSummary{}

// statefulSummary implements block.Summary by layering three objects:
// 1- summary.StatelessSummary carries all summary marshallable content along with
// data immediately retrievable from it.
// 2- summary.ProposerSummary adds to summary.StatelessSummary height, as retrieved
// by innerSummary
// 3- statefulSummary add to summary.ProposerSummary the implementation
// of block.Summary.Accept, to handle processing of the validated summary.
// Note that summary.StatelessSummary contains data to build both innerVM summary
// and the full proposerVM block associated with the summary.
type statefulSummary struct {
	summary.ProposerSummary

	// inner summary, retrieved via Parse
	innerSummary block.Summary

	// block associated with the summary
	proposerBlock Block

	vm *VM
}

func (ss *statefulSummary) Accept() (bool, error) {
	// a non-empty statefulSummary carries the full proposerVM block associated
	// with the summary. We store this block and update height index with it,
	// so that state sync could resume after a shutdown.
	if ss.ID() != ids.Empty {
		if postForkBlk, ok := ss.proposerBlock.(PostForkBlock); ok {
			if err := ss.vm.storePostForkBlock(postForkBlk); err != nil {
				return false, err
			}
		}

		ss.vm.syncSummary = ss
	}

	return ss.innerSummary.Accept()
}
