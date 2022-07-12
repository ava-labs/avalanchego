// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

type BlockRejector interface {
	RejectProposalBlock(b *ProposalBlock) error
	RejectAtomicBlock(b *AtomicBlock) error
	RejectStandardBlock(b *StandardBlock) error
	RejectCommitBlock(b *CommitBlock) error
	RejectAbortBlock(b *AbortBlock) error
}
