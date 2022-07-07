// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ Block = &CommitBlock{}

// TODO remove
// 	_ Decision = &CommitBlock{}

// CommitBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being enacted.
type CommitBlock struct {
	*stateless.CommitBlock
	*commonBlock
}

// NewCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID]. Additionally the block will track if it
// was originally preferred or not for metrics.
func NewCommitBlock(
	manager Manager,
	parentID ids.ID,
	height uint64,
) (*CommitBlock, error) {
	statelessBlk, err := stateless.NewCommitBlock(parentID, height)
	if err != nil {
		return nil, err
	}

	return toStatefulCommitBlock(statelessBlk, manager, choices.Processing)
}

func toStatefulCommitBlock(
	statelessBlk *stateless.CommitBlock,
	manager Manager,
	status choices.Status,
) (*CommitBlock, error) {
	commit := &CommitBlock{
		CommitBlock: statelessBlk,
		commonBlock: &commonBlock{
			Manager: manager,
			baseBlk: &statelessBlk.CommonBlock,
		},
	}

	return commit, nil
}

func (c *CommitBlock) Verify() error {
	return c.verifyCommitBlock(c)
}

func (c *CommitBlock) Accept() error {
	return c.acceptCommitBlock(c)
}

func (c *CommitBlock) Reject() error {
	return c.rejectCommitBlock(c)
}

func (c *CommitBlock) conflicts(s ids.Set) (bool, error) {
	return c.conflictsCommitBlock(c, s)
}

func (c *CommitBlock) setBaseState() {
	c.setBaseStateCommitBlock(c)
}
