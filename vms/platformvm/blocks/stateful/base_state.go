// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

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
	b.onCommitState.SetBase(s.state)
	b.onAbortState.SetBase(s.state)
}

func (s *baseStateSetterImpl) setBaseStateAtomicBlock(b *AtomicBlock) {
	if onAcceptState := s.blkIDToOnAcceptState[b.ID()]; onAcceptState != nil {
		onAcceptState.Apply(s.state)
	}
}

func (s *baseStateSetterImpl) setBaseStateStandardBlock(b *StandardBlock) {
	if onAcceptState := s.blkIDToOnAcceptState[b.ID()]; onAcceptState != nil {
		onAcceptState.Apply(s.state)
	}
}

func (s *baseStateSetterImpl) setBaseStateCommitBlock(b *CommitBlock) {
	if onAcceptState := s.blkIDToOnAcceptState[b.ID()]; onAcceptState != nil {
		onAcceptState.Apply(s.state)
	}
}

func (s *baseStateSetterImpl) setBaseStateAbortBlock(b *AbortBlock) {
	if onAcceptState := s.blkIDToOnAcceptState[b.ID()]; onAcceptState != nil {
		onAcceptState.Apply(s.state)
	}
}
