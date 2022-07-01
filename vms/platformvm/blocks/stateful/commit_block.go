// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_ Block    = &CommitBlock{}
	_ Decision = &CommitBlock{}
)

// CommitBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being enacted.
type CommitBlock struct {
	Manager // TODO set
	*stateless.CommitBlock
	*doubleDecisionBlock

	wasPreferred bool
}

// NewCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID]. Additionally the block will track if it
// was originally preferred or not for metrics.
func NewCommitBlock(
	manager Manager,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*CommitBlock, error) {
	statelessBlk, err := stateless.NewCommitBlock(parentID, height)
	if err != nil {
		return nil, err
	}

	return toStatefulCommitBlock(statelessBlk, manager, txExecutorBackend, wasPreferred, choices.Processing)
}

func toStatefulCommitBlock(
	statelessBlk *stateless.CommitBlock,
	manager Manager,
	txExecutorBackend executor.Backend,
	wasPreferred bool,
	status choices.Status,
) (*CommitBlock, error) {
	commit := &CommitBlock{
		CommitBlock: statelessBlk,
		Manager:     manager,
		doubleDecisionBlock: &doubleDecisionBlock{
			decisionBlock: decisionBlock{
				chainState: manager,
				commonBlock: &commonBlock{
					baseBlk:           &statelessBlk.CommonBlock,
					status:            status,
					txExecutorBackend: txExecutorBackend,
				},
			},
		},
		wasPreferred: wasPreferred,
	}

	return commit, nil
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
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

func (c *CommitBlock) free() {
	c.freeCommitBlock(c)
}

func (a *CommitBlock) setBaseState() {
	a.Manager.setBaseStateCommitBlock(a)
}
