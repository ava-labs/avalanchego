// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/forks"
)

var _ blocks.Visitor = &forkChecker{}

// forkChecker checks whether the provided block is valid in current fork
type forkChecker struct {
	*backend
}

func (f *forkChecker) BlueberryAbortBlock(b *blocks.BlueberryAbortBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Blueberry {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) BlueberryCommitBlock(b *blocks.BlueberryCommitBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Blueberry {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) BlueberryProposalBlock(b *blocks.BlueberryProposalBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Blueberry {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) BlueberryStandardBlock(b *blocks.BlueberryStandardBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Blueberry {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) ApricotAbortBlock(b *blocks.ApricotAbortBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Apricot {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) ApricotCommitBlock(b *blocks.ApricotCommitBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Apricot {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) ApricotProposalBlock(b *blocks.ApricotProposalBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Apricot {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) ApricotStandardBlock(b *blocks.ApricotStandardBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Apricot {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}

func (f *forkChecker) ApricotAtomicBlock(b *blocks.ApricotAtomicBlock) error {
	currentFork, err := f.GetFork(b.Parent())
	if err != nil {
		return fmt.Errorf("could not check block type %T against fork, %w", b, err)
	}

	if currentFork != forks.Apricot {
		return fmt.Errorf("block type %T not accepted on fork %s", b, currentFork)
	}
	return nil
}
