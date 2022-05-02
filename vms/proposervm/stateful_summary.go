// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
)

var _ block.StateSummary = &postForkStatefulSummary{}

// postForkStatefulSummary implements block.StateSummary by layering three objects:
// 1- summary.StatelessSummary carries all summary marshallable content along with
// data immediately retrievable from it.
// 2- summary.ProposerSummary adds to summary.StatelessSummary height, as retrieved
// by innerSummary
// 3- postForkStatefulSummary add to summary.ProposerSummary the implementation
// of block.StateSummary.Accept, to handle processing of the validated summary.
// Note that summary.StatelessSummary contains data to build both innerVM summary
// and the full proposerVM block associated with the summary.
type postForkStatefulSummary struct {
	statelessSummary summary.StateSummary

	// inner summary, retrieved via Parse
	innerSummary block.StateSummary

	// block associated with the summary
	proposerBlock Block
}

func (s *postForkStatefulSummary) ID() ids.ID {
	return s.statelessSummary.ID()
}

func (s *postForkStatefulSummary) Height() uint64 {
	return s.innerSummary.Height()
}

func (s *postForkStatefulSummary) Bytes() []byte {
	return s.statelessSummary.Bytes()
}

func (s *postForkStatefulSummary) Accept() (bool, error) {
	// a statefulSummary carries the full proposerVM block associated
	// with the summary. We store this block and update height index with it,
	// so that state sync could resume after a shutdown.
	if err := s.proposerBlock.acceptOuterBlk(); err != nil {
		return false, err
	}
	return s.innerSummary.Accept()
}
