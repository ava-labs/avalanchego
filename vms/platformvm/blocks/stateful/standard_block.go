// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	errConflictingBatchTxs = errors.New("block contains conflicting transactions")

	_ Block    = &StandardBlock{}
	_ Decision = &StandardBlock{}
)

// StandardBlock being accepted results in the transactions contained in the
// block to be accepted and committed to the chain.
type StandardBlock struct {
	*stateless.StandardBlock
	*decisionBlock

	// Inputs are the atomic Inputs that are consumed by this block's atomic
	// transactions
	Inputs         ids.Set
	atomicRequests map[ids.ID]*atomic.Requests

	manager Manager
}

// NewStandardBlock returns a new *StandardBlock where the block's parent, a
// decision block, has ID [parentID].
func NewStandardBlock(
	manager Manager,
	ctx *snow.Context,
	parentID ids.ID,
	height uint64,
	txs []*txs.Tx,
) (*StandardBlock, error) {
	statelessBlk, err := stateless.NewStandardBlock(parentID, height, txs)
	if err != nil {
		return nil, err
	}
	return toStatefulStandardBlock(statelessBlk, manager, ctx, choices.Processing)
}

func toStatefulStandardBlock(
	statelessBlk *stateless.StandardBlock,
	manager Manager,
	ctx *snow.Context,
	status choices.Status,
) (*StandardBlock, error) {
	sb := &StandardBlock{
		StandardBlock: statelessBlk,
		decisionBlock: &decisionBlock{
			chainState: manager,
			commonBlock: &commonBlock{
				timestampGetter: manager,
				LastAccepteder:  manager,
				baseBlk:         &statelessBlk.CommonBlock,
				status:          status,
			},
		},
		manager: manager,
	}

	for _, tx := range sb.Txs {
		tx.Unsigned.InitCtx(ctx)
	}

	return sb, nil
}

// conflicts checks to see if the provided input set contains any conflicts with
// any of this block's non-accepted ancestors or itself.
func (sb *StandardBlock) conflicts(s ids.Set) (bool, error) {
	return sb.manager.conflictsStandardBlock(sb, s)
}

func (sb *StandardBlock) Verify() error {
	return sb.manager.verifyStandardBlock(sb)
}

func (sb *StandardBlock) Accept() error {
	return sb.manager.acceptStandardBlock(sb)
}

func (sb *StandardBlock) Reject() error {
	return sb.manager.rejectStandardBlock(sb)
}

func (sb *StandardBlock) free() {
	sb.manager.freeStandardBlock(sb)
}

func (sb *StandardBlock) setBaseState() {
	sb.manager.setBaseStateStandardBlock(sb)
}
