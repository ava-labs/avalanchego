// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

var _ freer = &freerImpl{}

type freer interface {
	// TODO is this interface right?
	// What about other block types?
	freeProposalBlock(b *ProposalBlock)
	freeDecisionBlock(b *decisionBlock)
	freeCommonBlock(b *commonBlock)
}

func NewFreer() freer {
	// TODO implement
	return &freerImpl{}
}

type freerImpl struct {
	backend
}

func (f *freerImpl) freeProposalBlock(b *ProposalBlock) {
	f.freeCommonBlock(b.commonBlock)
	b.onCommitState = nil
	b.onAbortState = nil
}

func (f *freerImpl) freeDecisionBlock(b *decisionBlock) {
	f.freeCommonBlock(b.commonBlock)
	b.onAcceptState = nil
}

func (f *freerImpl) freeCommonBlock(b *commonBlock) {
	f.dropVerifiedBlock(b.baseBlk.ID())
	b.children = nil
}
