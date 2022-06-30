// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ conflictChecker = &conflictCheckerImpl{}

type conflictChecker interface {
	conflictsAtomicBlock(b *AtomicBlock, s ids.Set) (bool, error)
	conflictsStandardBlock(b *StandardBlock, s ids.Set) (bool, error)
	conflictsCommonBlock(b *commonBlock, s ids.Set) (bool, error)
}

type conflictCheckerImpl struct{}

func newConflictChecker() conflictChecker {
	return &conflictCheckerImpl{}
}

func (c *conflictCheckerImpl) conflictsAtomicBlock(b *AtomicBlock, s ids.Set) (bool, error) {
	if b.Status() == choices.Accepted {
		return false, nil
	}
	if b.inputs.Overlaps(s) {
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
	if b.Inputs.Overlaps(s) {
		return true, nil
	}
	parent, err := c.parent(b.baseBlk)
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

func (c *conflictCheckerImpl) conflictsCommonBlock(b *commonBlock, s ids.Set) (bool, error) {
	if b.Status() == choices.Accepted {
		return false, nil
	}
	parent, err := c.parent(b.baseBlk)
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

func (c *conflictCheckerImpl) parent(b *stateless.CommonBlock) (*commonBlock, error) {
	return nil, errors.New("TODO")
}
