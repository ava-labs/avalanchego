// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

type Visitor interface {
	AtomicBlock(blk *AtomicBlock) error
	ProposalBlock(blk *ProposalBlock) error
	StandardBlock(blk *StandardBlock) error
	AbortBlock(blk *AbortBlock) error
	CommitBlock(blk *CommitBlock) error
}
