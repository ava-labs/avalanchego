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
	_ Block    = &AbortBlock{}
	_ Decision = &AbortBlock{}
)

// AbortBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being rejected.
type AbortBlock struct {
	Manager
	*stateless.AbortBlock
	*doubleDecisionBlock

	wasPreferred bool
}

// NewAbortBlock returns a new *AbortBlock where the block's parent, a proposal
// block, has ID [parentID]. Additionally the block will track if it was
// originally preferred or not for metrics.
func NewAbortBlock(
	manager Manager,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*AbortBlock, error) {
	statelessBlk, err := stateless.NewAbortBlock(parentID, height)
	if err != nil {
		return nil, err
	}
	return toStatefulAbortBlock(
		statelessBlk,
		manager,
		txExecutorBackend,
		wasPreferred,
		choices.Processing,
	)
}

func toStatefulAbortBlock(
	statelessBlk *stateless.AbortBlock,
	manager Manager,
	txExecutorBackend executor.Backend,
	wasPreferred bool,
	status choices.Status,
) (*AbortBlock, error) {
	abort := &AbortBlock{
		AbortBlock: statelessBlk,
		Manager:    manager,
		doubleDecisionBlock: &doubleDecisionBlock{
			decisionBlock: decisionBlock{
				commonBlock: &commonBlock{
					baseBlk:           &statelessBlk.CommonBlock,
					status:            status,
					txExecutorBackend: txExecutorBackend,
				},
			},
		},
		wasPreferred: wasPreferred,
	}

	return abort, nil
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
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
