// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ Block = &AbortBlock{}

// TODO remove
// _ Decision = &AbortBlock{}

// AbortBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being rejected.
type AbortBlock struct {
	*stateless.AbortBlock
	*commonBlock
}

// NewAbortBlock returns a new *AbortBlock where the block's parent, a proposal
// block, has ID [parentID]. Additionally the block will track if it was
// originally preferred or not for metrics.
func NewAbortBlock(
	manager Manager,
	parentID ids.ID,
	height uint64,
) (*AbortBlock, error) {
	statelessBlk, err := stateless.NewAbortBlock(parentID, height)
	if err != nil {
		return nil, err
	}
	return toStatefulAbortBlock(
		statelessBlk,
		manager,
		choices.Processing,
	)
}

func toStatefulAbortBlock(
	statelessBlk *stateless.AbortBlock,
	manager Manager,
	status choices.Status,
) (*AbortBlock, error) {
	abort := &AbortBlock{
		AbortBlock: statelessBlk,
		commonBlock: &commonBlock{
			Manager: manager,
			baseBlk: &statelessBlk.CommonBlock,
		},
	}

	return abort, nil
}

func (a *AbortBlock) Verify() error {
	return a.verifyAbortBlock(a)
}

func (a *AbortBlock) Accept() error {
	return a.acceptAbortBlock(a)
}

func (a *AbortBlock) Reject() error {
	return a.rejectAbortBlock(a)
}

func (a *AbortBlock) conflicts(s ids.Set) (bool, error) {
	return a.conflictsAbortBlock(a, s)
}

func (a *AbortBlock) setBaseState() {
	a.setBaseStateAbortBlock(a)
}
