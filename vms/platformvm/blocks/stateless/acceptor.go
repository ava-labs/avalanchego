// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

type BlockAcceptor interface {
	AcceptProposalBlock(b *ProposalBlock) error
	AcceptAtomicBlock(b *AtomicBlock) error
	AcceptStandardBlock(b *StandardBlock) error
	AcceptCommitBlock(b *CommitBlock) error
	AcceptAbortBlock(b *AbortBlock) error
}
