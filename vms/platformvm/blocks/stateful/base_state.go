// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "github.com/ava-labs/avalanchego/vms/platformvm/state"

var _ baseStateSetter = &baseStateSetterImpl{}

type baseStateSetter interface {
	setBaseStateProposalBlock(b *ProposalBlock)
	setBaseStateAtomicBlock(b *AtomicBlock)
	setBaseStateStandardBlock(b *StandardBlock)
	setBaseStateCommitBlock(b *CommitBlock)
	setBaseStateAbortBlock(b *AbortBlock)
}

type baseStateSetterImpl struct {
	state.State
}

func (s *baseStateSetterImpl) setBaseStateProposalBlock(b *ProposalBlock) {
	b.onCommitState.SetBase(s.State)
	b.onAbortState.SetBase(s.State)
}

func (s *baseStateSetterImpl) setBaseStateAtomicBlock(b *AtomicBlock) {
	b.onAcceptState.SetBase(s.State)
}

func (s *baseStateSetterImpl) setBaseStateStandardBlock(b *StandardBlock) {
	b.onAcceptState.SetBase(s.State)
}

func (s *baseStateSetterImpl) setBaseStateCommitBlock(b *CommitBlock) {
	b.onAcceptState.SetBase(s.State)
}

func (s *baseStateSetterImpl) setBaseStateAbortBlock(b *AbortBlock) {
	b.onAcceptState.SetBase(s.State)
}
