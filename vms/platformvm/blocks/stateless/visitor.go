// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

type Visitor interface {
	VisitAtomicBlock(blk *AtomicBlock) error
	VisitApricotProposalBlock(blk *ApricotProposalBlock) error
	VisitBlueberryProposalBlock(blk *BlueberryProposalBlock) error
	VisitApricotStandardBlock(blk *ApricotStandardBlock) error
	VisitBlueberryStandardBlock(blk *BlueberryStandardBlock) error
	VisitAbortBlock(blk *AbortBlock) error
	VisitCommitBlock(blk *CommitBlock) error
}
