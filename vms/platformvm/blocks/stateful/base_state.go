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
	blockState, ok := s.blkIDToState[blkID]
	if !ok {
		return
	}
	blockState.onAcceptState.Apply(s.state)
	// if onAcceptState := s.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
	// 	onAcceptState.Apply(s.state)
	// }
}
