// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

type Visitor interface {
	VisitAtomicBlock(blk *AtomicBlock) error
	VisitProposalBlock(blk *ProposalBlock) error
	VisitStandardBlock(blk *StandardBlock) error
	VisitAbortBlock(blk *AbortBlock) error
	VisitCommitBlock(blk *CommitBlock) error
}
