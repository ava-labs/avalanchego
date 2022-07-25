// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
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
	CommonBlock `serialize:"true"`

	Tx *txs.Tx `serialize:"true" json:"tx"`

	// The state that the chain will have if this block's proposal is committed
	onCommitState state.Diff
	// The state that the chain will have if this block's proposal is aborted
	onAbortState  state.Diff
	prefersCommit bool
}

func (pb *ProposalBlock) free() {
	pb.CommonBlock.free()
	pb.onCommitState = nil
	pb.onAbortState = nil
}

func (pb *ProposalBlock) Accept() error {
	blkID := pb.ID()
	pb.vm.ctx.Log.Verbo("accepting Proposal Block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", pb.Height()),
		zap.Stringer("parentID", pb.Parent()),
	)

	pb.status = choices.Accepted
	pb.vm.lastAcceptedID = blkID
	return nil
}

func (pb *ProposalBlock) Reject() error {
	pb.vm.ctx.Log.Verbo("rejecting Proposal Block",
		zap.Stringer("blkID", pb.ID()),
		zap.Uint64("height", pb.Height()),
		zap.Stringer("parentID", pb.Parent()),
	)

	pb.onCommitState = nil
	pb.onAbortState = nil

	if err := pb.vm.blockBuilder.AddVerifiedTx(pb.Tx); err != nil {
		pb.vm.ctx.Log.Verbo("failed to reissue tx",
			zap.Stringer("txID", pb.Tx.ID()),
			zap.Error(err),
		)
	}
	return pb.CommonBlock.Reject()
}

func (pb *ProposalBlock) initialize(vm *VM, bytes []byte, status choices.Status, self Block) error {
	if err := pb.CommonBlock.initialize(vm, bytes, status, self); err != nil {
		return err
	}
	if err := pb.Tx.Sign(Codec, nil); err != nil {
		return fmt.Errorf("failed to sign block: %w", err)
	}
	pb.Tx.Unsigned.InitCtx(vm.ctx)
	return nil
}

// Verify this block is valid.
//
// The parent block must be a decision block
//
// If this block is valid, this function also sets pas.onCommit and pas.onAbort.
func (pb *ProposalBlock) Verify() error {
	blkID := pb.ID()

	if err := pb.CommonBlock.Verify(); err != nil {
		return err
	}

	txExecutor := executor.ProposalTxExecutor{
		Backend:  &pb.vm.txExecutorBackend,
		ParentID: pb.PrntID,
		Tx:       pb.Tx,
	}
	err := pb.Tx.Unsigned.Visit(&txExecutor)
	if err != nil {
		txID := pb.Tx.ID()
		pb.vm.blockBuilder.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	pb.onCommitState = txExecutor.OnCommit
	pb.onAbortState = txExecutor.OnAbort
	pb.prefersCommit = txExecutor.PrefersCommit

	pb.onCommitState.AddTx(pb.Tx, status.Committed)
	pb.onAbortState.AddTx(pb.Tx, status.Aborted)

	// It is safe to use [pb.onAbortState] here because the timestamp will never
	// be modified by an Abort block.
	pb.timestamp = pb.onAbortState.GetTimestamp()

	pb.vm.blockBuilder.RemoveProposalTx(pb.Tx)
	pb.vm.currentBlocks[blkID] = pb

	// Notice that we do not add an entry to the state versions here for this
	// block. This block must be followed by either a Commit or an Abort block.
	// These blocks will get their parent state by referencing [onCommitState]
	// or [onAbortState] directly.
	return nil
}

// Options returns the possible children of this block in preferential order.
func (pb *ProposalBlock) Options() ([2]snowman.Block, error) {
	blkID := pb.ID()
	nextHeight := pb.Height() + 1

	commit, err := pb.vm.newCommitBlock(blkID, nextHeight, pb.prefersCommit)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}
	abort, err := pb.vm.newAbortBlock(blkID, nextHeight, !pb.prefersCommit)
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

// newProposalBlock creates a new block that proposes to issue a transaction.
//
// The parent of this block has ID [parentID].
//
// The parent must be a decision block.
func (vm *VM) newProposalBlock(parentID ids.ID, height uint64, tx *txs.Tx) (*ProposalBlock, error) {
	pb := &ProposalBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}

	// We marshal the block in this way (as a Block) so that we can unmarshal
	// it into a Block (rather than a *ProposalBlock)
	block := Block(pb)
	bytes, err := Codec.Marshal(txs.Version, &block)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return pb, pb.CommonBlock.initialize(vm, bytes, choices.Processing, pb)
}
