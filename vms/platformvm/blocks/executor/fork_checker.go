// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/forks"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ blocks.Visitor = &forkChecker{}

// forkChecker checks whether the provided block is valid in current fork
// It also carries out fork specific checks, i.e. checks that apply to all
// blocks of a specific fork but not to other forks.
type forkChecker struct {
	*backend
	clk *mockable.Clock
}

func (f *forkChecker) BlueberryAbortBlock(b *blocks.BlueberryAbortBlock) error {
	if err := f.assertFork(b.Parent(), forks.Blueberry); err != nil {
		return err
	}

	return f.validateBlueberryOptionsTimestamp(b)
}

func (f *forkChecker) BlueberryCommitBlock(b *blocks.BlueberryCommitBlock) error {
	if err := f.assertFork(b.Parent(), forks.Blueberry); err != nil {
		return err
	}

	return f.validateBlueberryOptionsTimestamp(b)
}

func (f *forkChecker) BlueberryProposalBlock(b *blocks.BlueberryProposalBlock) error {
	if err := f.assertFork(b.Parent(), forks.Blueberry); err != nil {
		return err
	}

	return f.validateBlueberryBlocksTimestamp(b)
}

func (f *forkChecker) BlueberryStandardBlock(b *blocks.BlueberryStandardBlock) error {
	if err := f.assertFork(b.Parent(), forks.Blueberry); err != nil {
		return err
	}

	return f.validateBlueberryBlocksTimestamp(b)
}

func (f *forkChecker) ApricotAbortBlock(b *blocks.ApricotAbortBlock) error {
	return f.assertFork(b.Parent(), forks.Apricot)
}

func (f *forkChecker) ApricotCommitBlock(b *blocks.ApricotCommitBlock) error {
	return f.assertFork(b.Parent(), forks.Apricot)
}

func (f *forkChecker) ApricotProposalBlock(b *blocks.ApricotProposalBlock) error {
	return f.assertFork(b.Parent(), forks.Apricot)
}

func (f *forkChecker) ApricotStandardBlock(b *blocks.ApricotStandardBlock) error {
	return f.assertFork(b.Parent(), forks.Apricot)
}

func (f *forkChecker) ApricotAtomicBlock(b *blocks.ApricotAtomicBlock) error {
	return f.assertFork(b.Parent(), forks.Apricot)
}

func (f *forkChecker) assertFork(parent ids.ID, expectedFork forks.Fork) error {
	currentFork, err := f.GetFork(parent)
	if err != nil {
		return fmt.Errorf("couldn't get fork from parent %s: %w", parent, err)
	}
	if currentFork != expectedFork {
		return fmt.Errorf("expected fork %d but got %d", expectedFork, currentFork)
	}
	return nil
}

func (f *forkChecker) validateBlueberryOptionsTimestamp(b blocks.Block) error {
	parentID := b.Parent()
	parentBlk, err := f.getStatelessBlock(parentID)
	if err != nil {
		return err
	}
	parentBlkTime := parentBlk.Timestamp()
	blkTime := b.Timestamp()

	if !blkTime.Equal(parentBlkTime) {
		return fmt.Errorf(
			"%w parent block timestamp (%s) option block timestamp (%s)",
			errOptionBlockTimestampNotMatchingParent,
			parentBlkTime,
			blkTime,
		)
	}
	return nil
}

func (f *forkChecker) validateBlueberryBlocksTimestamp(b blocks.Block) error {
	parentID := b.Parent()
	parentBlk, err := f.getStatelessBlock(parentID)
	if err != nil {
		return err
	}
	parentBlkTime := parentBlk.Timestamp()
	blkTime := b.Timestamp()

	parentState, ok := f.GetState(parentID)
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, b.ID())
	}
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(parentState)
	if err != nil {
		return fmt.Errorf("could not verify block timestamp: %w", err)
	}
	localTime := f.clk.Time()

	return executor.ValidateProposedChainTime(
		blkTime,
		parentBlkTime,
		nextStakerChangeTime,
		localTime,
		false, /*enforceStrictness*/
	)
}
