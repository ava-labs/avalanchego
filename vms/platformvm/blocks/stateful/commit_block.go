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
	// TODO set this field
	verifier2 verifier
	// TODO set this field
	acceptor acceptor
	// TODO set this field
	rejector rejector
	*stateless.CommitBlock
	*doubleDecisionBlock

	wasPreferred bool
}

// NewCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID]. Additionally the block will track if it
// was originally preferred or not for metrics.
func NewCommitBlock(
	verifier verifier,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*CommitBlock, error) {
	statelessBlk, err := stateless.NewCommitBlock(parentID, height)
	if err != nil {
		return nil, err
	}

	return toStatefulCommitBlock(statelessBlk, verifier, txExecutorBackend, wasPreferred, choices.Processing)
}

func toStatefulCommitBlock(
	statelessBlk *stateless.CommitBlock,
	verifier verifier,
	txExecutorBackend executor.Backend,
	wasPreferred bool,
	status choices.Status,
) (*CommitBlock, error) {
	commit := &CommitBlock{
		CommitBlock: statelessBlk,
		doubleDecisionBlock: &doubleDecisionBlock{
			decisionBlock: decisionBlock{
				commonBlock: &commonBlock{
					baseBlk:           &statelessBlk.CommonBlock,
					status:            status,
					verifier:          verifier,
					txExecutorBackend: txExecutorBackend,
				},
			},
		},
		wasPreferred: wasPreferred,
	}

	return commit, nil
}

func (c *CommitBlock) Accept() error {
	return c.acceptor.acceptCommitBlock(c)
}

func (c *CommitBlock) Reject() error {
	return c.rejector.rejectCommitBlock(c)
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
func (c *CommitBlock) Verify() error {
	return c.verifier2.verifyCommitBlock(c)
}
