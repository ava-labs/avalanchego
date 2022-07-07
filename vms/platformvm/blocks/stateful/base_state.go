// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ baseStateSetter = &baseStateSetterImpl{}

type baseStateSetter interface {
	setBaseStateProposalBlock(b *stateless.ProposalBlock)
	setBaseStateAtomicBlock(b *stateless.AtomicBlock)
	setBaseStateStandardBlock(b *stateless.StandardBlock)
	setBaseStateCommitBlock(b *stateless.CommitBlock)
	setBaseStateAbortBlock(b *stateless.AbortBlock)
}

type baseStateSetterImpl struct {
	backend
}

func (s *baseStateSetterImpl) setBaseStateProposalBlock(b *stateless.ProposalBlock) {
	blockState, ok := s.blkIDToState[b.ID()]
	if !ok {
		return
	}
	blockState.onCommitState.SetBase(s.state)
	blockState.onAbortState.SetBase(s.state)
	// if onCommitState := s.blkIDToOnCommitState[b.ID()]; onCommitState != nil {
	// 	onCommitState.SetBase(s.state)
	// }
	// if onAbortState := s.blkIDToOnAbortState[b.ID()]; onAbortState != nil {
	// 	onAbortState.SetBase(s.state)
	// }
}

func (s *baseStateSetterImpl) setBaseStateAtomicBlock(b *stateless.AtomicBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateStandardBlock(b *stateless.StandardBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateCommitBlock(b *stateless.CommitBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateAbortBlock(b *stateless.AbortBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateCommon(blkID ids.ID) {
	blockState, ok := s.blkIDToState[blkID]
	if !ok {
		return
	}
	blockState.onAcceptState.Apply(s.state)
	// if onAcceptState := s.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
	// 	onAcceptState.Apply(s.state)
	// }
}
