// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "github.com/ava-labs/avalanchego/ids"

var _ baseStateSetter = &baseStateSetterImpl{}

type baseStateSetter interface {
	setBaseStateProposalBlock(b *ProposalBlock)
	setBaseStateAtomicBlock(b *AtomicBlock)
	setBaseStateStandardBlock(b *StandardBlock)
	setBaseStateCommitBlock(b *CommitBlock)
	setBaseStateAbortBlock(b *AbortBlock)
}

type baseStateSetterImpl struct {
	backend
}

func (s *baseStateSetterImpl) setBaseStateProposalBlock(b *ProposalBlock) {
	if onCommitState := s.blkIDToOnCommitState[b.ID()]; onCommitState != nil {
		onCommitState.SetBase(s.state)
	}
	if onAbortState := s.blkIDToOnAbortState[b.ID()]; onAbortState != nil {
		onAbortState.SetBase(s.state)
	}
}

func (s *baseStateSetterImpl) setBaseStateAtomicBlock(b *AtomicBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateStandardBlock(b *StandardBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateCommitBlock(b *CommitBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateAbortBlock(b *AbortBlock) {
	s.setBaseStateCommon(b.ID())
}

func (s *baseStateSetterImpl) setBaseStateCommon(blkID ids.ID) {
	if onAcceptState := s.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
		onAcceptState.Apply(s.state)
	}
}
