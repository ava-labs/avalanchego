// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

type Visitor interface {
	BanffAbortBlock(*BanffAbortBlock) error
	BanffCommitBlock(*BanffCommitBlock) error
	BanffProposalBlock(*BanffProposalBlock) error
	BanffStandardBlock(*BanffStandardBlock) error

	ApricotAbortBlock(*ApricotAbortBlock) error
	ApricotCommitBlock(*ApricotCommitBlock) error
	ApricotProposalBlock(*ApricotProposalBlock) error
	ApricotStandardBlock(*ApricotStandardBlock) error
	ApricotAtomicBlock(*ApricotAtomicBlock) error
}
