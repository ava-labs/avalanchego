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
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
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
	stateless.ProposalBlockIntf
	*commonBlock

	// The state that the chain will have if this block's proposal is committed
	onCommitState state.Versioned
	// The state that the chain will have if this block's proposal is aborted
	onAbortState state.Versioned

	prefersCommit bool
}

// NewProposalBlock creates a new block that proposes to issue a transaction.
// The parent of this block has ID [parentID].
// The parent must be a decision block.
func NewProposalBlock(
	version uint16,
	timestamp uint64,
	verifier Verifier,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	tx txs.Tx,
) (*ProposalBlock, error) {
	statelessBlk, err := stateless.NewProposalBlock(version, timestamp, parentID, height, tx)
	if err != nil {
		return nil, err
	}

	return toStatefulProposalBlock(statelessBlk, verifier, txExecutorBackend, choices.Processing)
}

func toStatefulProposalBlock(
	statelessBlk stateless.ProposalBlockIntf,
	verifier Verifier,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (*ProposalBlock, error) {
	pb := &ProposalBlock{
		ProposalBlockIntf: statelessBlk,
		commonBlock: &commonBlock{
			commonStatelessBlk: statelessBlk,
			status:             status,
			verifier:           verifier,
			txExecutorBackend:  txExecutorBackend,
		},
	}

	pb.ProposalTx().Unsigned.InitCtx(pb.txExecutorBackend.Ctx)
	return pb, nil
}

func (pb *ProposalBlock) free() {
	pb.commonBlock.free()
	pb.onCommitState = nil
	pb.onAbortState = nil
}

func (pb *ProposalBlock) Accept() error {
	blkID := pb.ID()
	pb.txExecutorBackend.Ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		pb.Height(),
		pb.Parent(),
	)

	pb.status = choices.Accepted
	pb.verifier.SetLastAccepted(blkID)
	return nil
}

func (pb *ProposalBlock) Reject() error {
	pb.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Proposal Block %s at height %d with parent %s",
		pb.ID(),
		pb.Height(),
		pb.Parent(),
	)

	pb.onCommitState = nil
	pb.onAbortState = nil

	tx := pb.ProposalTx()
	if err := pb.verifier.Add(tx); err != nil {
		pb.txExecutorBackend.Ctx.Log.Verbo(
			"failed to reissue tx %q due to: %s",
			tx.ID(),
			err,
		)
	}

	defer pb.reject()
	pb.verifier.AddStatelessBlock(pb.ProposalBlockIntf, pb.Status())
	return pb.verifier.Commit()
}

func (pb *ProposalBlock) setBaseState() {
	pb.onCommitState.SetBase(pb.verifier)
	pb.onAbortState.SetBase(pb.verifier)
}

// Verify this block is valid.
//
// The parent block must either be a Commit or an Abort block.
//
// If this block is valid, this function also sets pas.onCommit and pas.onAbort.
func (pb *ProposalBlock) Verify() error {
	if err := pb.verify(); err != nil {
		return err
	}

	parentIntf, parentErr := pb.parentBlock()
	if parentErr != nil {
		return parentErr
	}

	// The parent of a proposal block (ie this block) must be a decision block
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}

	// parentState is the state if this block's parent is accepted
	parentState := parent.OnAccept()
	tx := pb.ProposalTx()
	txExecutor := executor.ProposalTxExecutor{
		Backend:     &pb.txExecutorBackend,
		ParentState: parentState,
		Tx:          tx,
	}
	err := tx.Unsigned.Visit(&txExecutor)
	if err != nil {
		txID := tx.ID()
		pb.verifier.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	pb.onCommitState = txExecutor.OnCommit
	pb.onAbortState = txExecutor.OnAbort
	pb.prefersCommit = txExecutor.PrefersCommit

	pb.onCommitState.AddTx(tx, status.Committed)
	pb.onAbortState.AddTx(tx, status.Aborted)

	pb.SetTimestamp(parentState.GetTimestamp())

	pb.verifier.RemoveProposalTx(tx)
	pb.verifier.CacheVerifiedBlock(pb)
	parentIntf.addChild(pb)
	return nil
}

// Options returns the possible children of this block in preferential order.
func (pb *ProposalBlock) Options() ([2]snowman.Block, error) {
	blkID := pb.ID()
	nextHeight := pb.Height() + 1

	commit, err := NewCommitBlock(
		pb.Version(),
		uint64(pb.Timestamp().Unix()),
		pb.verifier,
		pb.txExecutorBackend,
		blkID,
		nextHeight,
		pb.prefersCommit,
	)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}
	abort, err := NewAbortBlock(
		pb.Version(),
		uint64(pb.Timestamp().Unix()),
		pb.verifier,
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
