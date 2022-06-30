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
	// TODO set this field
	verifier2 Verifier2
	// TODO set this field
	acceptor Acceptor
	// TODO set this field
	rejector Rejector
	*stateless.AbortBlock
	*doubleDecisionBlock

	wasPreferred bool
}

// NewAbortBlock returns a new *AbortBlock where the block's parent, a proposal
// block, has ID [parentID]. Additionally the block will track if it was
// originally preferred or not for metrics.
func NewAbortBlock(
	verifier Verifier2,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*AbortBlock, error) {
	statelessBlk, err := stateless.NewAbortBlock(parentID, height)
	if err != nil {
		return nil, err
	}
	return toStatefulAbortBlock(statelessBlk, verifier, txExecutorBackend, wasPreferred, choices.Processing)
}

func toStatefulAbortBlock(
	statelessBlk *stateless.AbortBlock,
	verifier Verifier2,
	txExecutorBackend executor.Backend,
	wasPreferred bool,
	status choices.Status,
) (*AbortBlock, error) {
	abort := &AbortBlock{
		AbortBlock: statelessBlk,
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

	return abort, nil
}

func (a *AbortBlock) Accept() error {
	return a.acceptor.acceptAbortBlock(a)
}

func (a *AbortBlock) Reject() error {
	return a.rejector.rejectAbortBlock(a)
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
func (a *AbortBlock) Verify() error {
	return a.verifier2.verifyAbortBlock(a)
}
