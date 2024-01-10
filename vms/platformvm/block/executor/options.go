// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

var _ block.Visitor = (*verifier)(nil)

// options supports build new option blocks
type options struct {
	// outputs populated by this struct's methods:
	commitBlock block.Block
	abortBlock  block.Block
}

func (*options) BanffAbortBlock(*block.BanffAbortBlock) error {
	return snowman.ErrNotOracle
}

func (*options) BanffCommitBlock(*block.BanffCommitBlock) error {
	return snowman.ErrNotOracle
}

func (o *options) BanffProposalBlock(b *block.BanffProposalBlock) error {
	timestamp := b.Timestamp()
	blkID := b.ID()
	nextHeight := b.Height() + 1

	var err error
	o.commitBlock, err = block.NewBanffCommitBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}

	o.abortBlock, err = block.NewBanffAbortBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}
	return nil
}

func (*options) BanffStandardBlock(*block.BanffStandardBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotAbortBlock(*block.ApricotAbortBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotCommitBlock(*block.ApricotCommitBlock) error {
	return snowman.ErrNotOracle
}

func (o *options) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	blkID := b.ID()
	nextHeight := b.Height() + 1

	var err error
	o.commitBlock, err = block.NewApricotCommitBlock(blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}

	o.abortBlock, err = block.NewApricotAbortBlock(blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}
	return nil
}

func (*options) ApricotStandardBlock(*block.ApricotStandardBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotAtomicBlock(*block.ApricotAtomicBlock) error {
	return snowman.ErrNotOracle
}
