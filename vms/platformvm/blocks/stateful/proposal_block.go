// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ Block = &ProposalBlock{}

// ProposalBlock is a proposal to change the chain's state.
//
// A proposal may be to:
// 	1. Advance the chain's timestamp (*AdvanceTimeTx)
//  2. Remove a staker from the staker set (*RewardStakerTx)
//  3. Add a new staker to the set of pending (future) stakers
//     (*AddValidatorTx, *AddDelegatorTx, *AddSubnetValidatorTx)
//
// The proposal will be enacted (change the chain's state) if the proposal block
// is accepted and followed by an accepted Commit block
type ProposalBlock struct {
	// TODO set this field
	verifier2 verifier
	// TODO set this field
	acceptor acceptor
	// TODO set this field
	rejector rejector
	// TODO set this field
	*stateless.ProposalBlock
	*commonBlock

	// The state that the chain will have if this block's proposal is committed
	onCommitState state.Diff
	// The state that the chain will have if this block's proposal is aborted
	onAbortState  state.Diff
	prefersCommit bool
}

// NewProposalBlock creates a new block that proposes to issue a transaction.
// The parent of this block has ID [parentID].
// The parent must be a decision block.
func NewProposalBlock(
	verifier verifier,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*ProposalBlock, error) {
	statelessBlk, err := stateless.NewProposalBlock(parentID, height, tx)
	if err != nil {
		return nil, err
	}

	return toStatefulProposalBlock(statelessBlk, verifier, txExecutorBackend, choices.Processing)
}

func toStatefulProposalBlock(
	statelessBlk *stateless.ProposalBlock,
	verifier verifier,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (*ProposalBlock, error) {
	pb := &ProposalBlock{
		ProposalBlock: statelessBlk,
		commonBlock: &commonBlock{
			baseBlk:           &statelessBlk.CommonBlock,
			status:            status,
			verifier:          verifier,
			txExecutorBackend: txExecutorBackend,
		},
	}

	pb.Tx.Unsigned.InitCtx(pb.txExecutorBackend.Ctx)
	return pb, nil
}

func (pb *ProposalBlock) free() {
	pb.freer.freeProposalBlock(pb)
	/* TODO remove
	pb.commonBlock.free()
	pb.onCommitState = nil
	pb.onAbortState = nil
	*/
}

func (pb *ProposalBlock) Accept() error {
	return pb.acceptor.acceptProposalBlock(pb)
}

func (pb *ProposalBlock) Reject() error {
	return pb.rejector.rejectProposalBlock(pb)
}

/* TODO remove
func (pb *ProposalBlock) setBaseState() {
	pb.onCommitState.SetBase(pb.verifier.GetChainState())
	pb.onAbortState.SetBase(pb.verifier.GetChainState())
}
*/

// Verify this block is valid.
//
// The parent block must either be a Commit or an Abort block.
//
// If this block is valid, this function also sets pas.onCommit and pas.onAbort.
func (pb *ProposalBlock) Verify() error {
	return pb.verifier2.verifyProposalBlock(pb)
}

// Options returns the possible children of this block in preferential order.
func (pb *ProposalBlock) Options() ([2]snowman.Block, error) {
	blkID := pb.ID()
	nextHeight := pb.Height() + 1

	commit, err := NewCommitBlock(pb.verifier, pb.txExecutorBackend, blkID, nextHeight, pb.prefersCommit)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}
	abort, err := NewAbortBlock(
		pb.verifier,
		nil,
		nil,
		nil,
		pb.txExecutorBackend,
		blkID,
		nextHeight,
		!pb.prefersCommit,
	)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}

	if pb.prefersCommit {
		return [2]snowman.Block{commit, abort}, nil
	}
	return [2]snowman.Block{abort, commit}, nil
}
