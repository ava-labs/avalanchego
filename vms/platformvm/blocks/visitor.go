// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

type Visitor interface {
	AtomicBlock(blk *AtomicBlock) error
	ApricotProposalBlock(blk *ApricotProposalBlock) error
	BlueberryProposalBlock(blk *BlueberryProposalBlock) error
	ApricotStandardBlock(blk *ApricotStandardBlock) error
	BlueberryStandardBlock(blk *BlueberryStandardBlock) error
	AbortBlock(blk *AbortBlock) error
	CommitBlock(blk *CommitBlock) error
}
