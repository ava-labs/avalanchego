// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var _ conflictChecker = &conflictCheckerImpl{}

type conflictChecker interface {
	conflictsProposalBlock(b *ProposalBlock, s ids.Set) (bool, error)
	conflictsAtomicBlock(b *AtomicBlock, s ids.Set) (bool, error)
	conflictsCommitBlock(b *CommitBlock, s ids.Set) (bool, error)
	conflictsAbortBlock(b *AbortBlock, s ids.Set) (bool, error)
	conflictsStandardBlock(b *StandardBlock, s ids.Set) (bool, error)
	conflictsCommonBlock(b *commonBlock, s ids.Set) (bool, error)
}

type conflictCheckerImpl struct {
	backend
}

func (c *conflictCheckerImpl) conflictsAtomicBlock(b *AtomicBlock, s ids.Set) (bool, error) {
	if b.Status() == choices.Accepted {
		return false, nil
	}
	inputs := c.blkIDToInputs[b.ID()]
	if inputs.Overlaps(s) {
		return true, nil
	}
	parent, err := c.parent(b.baseBlk)
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

func (c *conflictCheckerImpl) conflictsStandardBlock(b *StandardBlock, s ids.Set) (bool, error) {
	if b.status == choices.Accepted {
		return false, nil
	}
	inputs := c.blkIDToInputs[b.ID()]
	if inputs.Overlaps(s) {
		return true, nil
	}
	parent, err := c.parent(b.baseBlk)
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

func (c *conflictCheckerImpl) conflictsProposalBlock(b *ProposalBlock, s ids.Set) (bool, error) {
	return c.conflictsCommonBlock(b.commonBlock, s)
}

func (c *conflictCheckerImpl) conflictsAbortBlock(b *AbortBlock, s ids.Set) (bool, error) {
	return c.conflictsCommonBlock(b.commonBlock, s)
}

func (c *conflictCheckerImpl) conflictsCommitBlock(b *CommitBlock, s ids.Set) (bool, error) {
	return c.conflictsCommonBlock(b.commonBlock, s)
}

func (c *conflictCheckerImpl) conflictsCommonBlock(b *commonBlock, s ids.Set) (bool, error) {
	if b.Status() == choices.Accepted {
		return false, nil
	}
	parent, err := c.backend.parent(b.baseBlk)
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}
