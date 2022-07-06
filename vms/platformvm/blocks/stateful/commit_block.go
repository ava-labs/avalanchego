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

	wasPreferred bool
	manager      Manager
}

// NewCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID]. Additionally the block will track if it
// was originally preferred or not for metrics.
func NewCommitBlock(
	manager Manager,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*CommitBlock, error) {
	statelessBlk, err := stateless.NewCommitBlock(parentID, height)
	if err != nil {
		return nil, err
	}

	return toStatefulCommitBlock(statelessBlk, manager, wasPreferred, choices.Processing)
}

func toStatefulCommitBlock(
	statelessBlk *stateless.CommitBlock,
	manager Manager,
	wasPreferred bool,
	status choices.Status,
) (*CommitBlock, error) {
	commit := &CommitBlock{
		CommitBlock: statelessBlk,
		commonBlock: &commonBlock{
			timestampGetter: manager,
			LastAccepteder:  manager,
			baseBlk:         &statelessBlk.CommonBlock,
			status:          status,
		},
		wasPreferred: wasPreferred,
		manager:      manager,
	}

	return commit, nil
}

func (c *CommitBlock) Verify() error {
	return c.manager.verifyCommitBlock(c)
}

func (c *CommitBlock) Accept() error {
	return c.manager.acceptCommitBlock(c)
}

func (c *CommitBlock) Reject() error {
	return c.manager.rejectCommitBlock(c)
}

func (c *CommitBlock) conflicts(s ids.Set) (bool, error) {
	return c.manager.conflictsCommitBlock(c, s)
}

func (c *CommitBlock) free() {
	c.manager.freeCommitBlock(c)
}

func (c *CommitBlock) setBaseState() {
	c.manager.setBaseStateCommitBlock(c)
}
