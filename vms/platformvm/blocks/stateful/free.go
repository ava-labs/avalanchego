// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

var _ freer = &freerImpl{}

type freer interface {
	freeProposalBlock(b *ProposalBlock)
	freeCommonBlock(b *commonBlock)
	freeAtomicBlock(b *AtomicBlock)
	freeCommitBlock(b *CommitBlock)
	freeAbortBlock(b *AbortBlock)
	freeStandardBlock(b *StandardBlock)
}

type freerImpl struct {
	backend
}

func (f *freerImpl) freeProposalBlock(b *ProposalBlock) {
	f.freeCommonBlock(b.commonBlock)
	b.onCommitState = nil
	b.onAbortState = nil
}

func (f *freerImpl) freeAtomicBlock(b *AtomicBlock) {
	f.freeCommonBlock(b.commonBlock)
}

func (f *freerImpl) freeAbortBlock(b *AbortBlock) {
	f.freeCommonBlock(b.commonBlock)
}

func (f *freerImpl) freeCommitBlock(b *CommitBlock) {
	f.freeCommonBlock(b.commonBlock)
}

func (f *freerImpl) freeStandardBlock(b *StandardBlock) {
	f.freeCommonBlock(b.commonBlock)
}

func (f *freerImpl) freeCommonBlock(b *commonBlock) {
	blkID := b.baseBlk.ID()
	delete(f.blkIDToOnAcceptFunc, blkID)
	delete(f.blkIDToOnAcceptState, blkID)
	f.unpinVerifiedBlock(blkID)
	b.children = nil
}
