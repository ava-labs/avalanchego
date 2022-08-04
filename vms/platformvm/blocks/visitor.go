// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

type Visitor interface {
	BlueberryAbortBlock(blk *BlueberryAbortBlock) error
	BlueberryCommitBlock(blk *BlueberryCommitBlock) error
	BlueberryProposalBlock(blk *BlueberryProposalBlock) error
	BlueberryStandardBlock(blk *BlueberryStandardBlock) error

	ApricotAbortBlock(blk *ApricotAbortBlock) error
	ApricotCommitBlock(blk *ApricotCommitBlock) error
	ApricotProposalBlock(blk *ApricotProposalBlock) error
	ApricotStandardBlock(blk *ApricotStandardBlock) error

	AtomicBlock(blk *AtomicBlock) error
}
