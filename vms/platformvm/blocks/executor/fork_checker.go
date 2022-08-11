// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/forks"
)

var _ blocks.Visitor = &forkChecker{}

// forkChecker checks whether the provided block is valid in current fork
type forkChecker struct {
	*backend
}

func (f *forkChecker) BlueberryAbortBlock(b *blocks.BlueberryAbortBlock) error {
	return f.assertFork(b.Parent(), forks.Blueberry)
}

func (f *forkChecker) BlueberryCommitBlock(b *blocks.BlueberryCommitBlock) error {
	return f.assertFork(b.Parent(), forks.Blueberry)
}

func (f *forkChecker) BlueberryProposalBlock(b *blocks.BlueberryProposalBlock) error {
	return f.assertFork(b.Parent(), forks.Blueberry)
}

func (f *forkChecker) BlueberryStandardBlock(b *blocks.BlueberryStandardBlock) error {
	return f.assertFork(b.Parent(), forks.Blueberry)
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
