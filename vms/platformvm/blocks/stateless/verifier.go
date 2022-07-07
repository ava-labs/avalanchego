// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

type BlockVerifier interface {
	// Verify this block is valid.
	// The parent block must either be a Commit or an Abort block.
	// If this block is valid, this function also sets pas.onCommit and pas.onAbort.
	VerifyProposalBlock(b *ProposalBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a decision block
	// This function also sets onAcceptDB database if the verification passes.
	VerifyAtomicBlock(b *AtomicBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a proposal
	// This function also sets onAcceptDB database if the verification passes.
	VerifyStandardBlock(b *StandardBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a proposal
	// This function also sets onAcceptState if the verification passes.
	VerifyCommitBlock(b *CommitBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a proposal
	// This function also sets onAcceptState if the verification passes.
	VerifyAbortBlock(b *AbortBlock) error
}
