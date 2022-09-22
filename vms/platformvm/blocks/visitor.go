// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

type Visitor interface {
	BlueberryAbortBlock(*BlueberryAbortBlock) error
	BlueberryCommitBlock(*BlueberryCommitBlock) error
	BlueberryProposalBlock(*BlueberryProposalBlock) error
	BlueberryStandardBlock(*BlueberryStandardBlock) error

	ApricotAbortBlock(*ApricotAbortBlock) error
	ApricotCommitBlock(*ApricotCommitBlock) error
	ApricotProposalBlock(*ApricotProposalBlock) error
	ApricotStandardBlock(*ApricotStandardBlock) error
	ApricotAtomicBlock(*ApricotAtomicBlock) error
}
