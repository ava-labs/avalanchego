// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "github.com/ava-labs/avalanchego/ids"

var _ Freer = &freer{}

type Freer interface {
	freeProposalBlock(b *ProposalBlock)
	freeDecisionBlock(b *decisionBlock)
	freeCommonBlock(b *commonBlock)
}

func NewFreer() Freer {
	// TODO implement
	return &freer{}
}

type freer struct {
	backend
}

func (f *freer) freeProposalBlock(b *ProposalBlock) {
	b.commonBlock.free()
	b.onCommitState = nil
	b.onAbortState = nil
}

func (f *freer) freeDecisionBlock(b *decisionBlock) {
	b.commonBlock.free()
	b.onAcceptState = nil
}

func (f *freer) freeCommonBlock(b *commonBlock) {
	f.dropVerifiedBlock(b.baseBlk.ID())
	b.children = nil
}

// TODO
func (f *freer) dropVerifiedBlock(id ids.ID) {}
