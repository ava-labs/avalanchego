// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
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

	Tx Tx `serialize:"true" json:"tx"`

	// The state that the chain will have if this block's proposal is committed
	onCommitState VersionedState
	// The state that the chain will have if this block's proposal is aborted
	onAbortState VersionedState
}

func (pb *ProposalBlock) free() {
	pb.CommonBlock.free()
	pb.onCommitState = nil
	pb.onAbortState = nil
}

func (pb *ProposalBlock) Accept() error {
	blkID := pb.ID()
	pb.vm.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		pb.Height(),
		pb.Parent(),
	)

	pb.status = choices.Accepted
	pb.vm.lastAcceptedID = blkID
	return nil
}

// Reject implements the snowman.Block interface
func (pb *ProposalBlock) Reject() error {
	pb.vm.ctx.Log.Verbo(
		"Rejecting Proposal Block %s at height %d with parent %s",
		pb.ID(),
		pb.Height(),
		pb.Parent(),
	)

	pb.onCommitState = nil
	pb.onAbortState = nil

	if err := pb.vm.blockBuilder.AddVerifiedTx(&pb.Tx); err != nil {
		pb.vm.ctx.Log.Verbo(
			"failed to reissue tx %q due to: %s",
			pb.Tx.ID(),
			err,
		)
	}
	return pb.CommonBlock.Reject()
}

func (pb *ProposalBlock) initialize(vm *VM, bytes []byte, status choices.Status, self Block) error {
	if err := pb.CommonBlock.initialize(vm, bytes, status, self); err != nil {
		return err
	}

	unsignedBytes, err := Codec.Marshal(CodecVersion, &pb.Tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("failed to marshal unsigned tx: %w", err)
	}
	signedBytes, err := Codec.Marshal(CodecVersion, &pb.Tx)
	if err != nil {
		return fmt.Errorf("failed to marshal tx: %w", err)
	}
	pb.Tx.Initialize(unsignedBytes, signedBytes)
	pb.Tx.InitCtx(vm.ctx)
	return nil
}

func (pb *ProposalBlock) setBaseState() {
	pb.onCommitState.SetBase(pb.vm.internalState)
	pb.onAbortState.SetBase(pb.vm.internalState)
}

// Verify this block is valid.
//
// The parent block must either be a Commit or an Abort block.
//
// If this block is valid, this function also sets pas.onCommit and pas.onAbort.
func (pb *ProposalBlock) Verify() error {
	blkID := pb.ID()

	if err := pb.CommonBlock.Verify(); err != nil {
		return err
	}

	tx, ok := pb.Tx.UnsignedTx.(UnsignedProposalTx)
	if !ok {
		return errWrongTxType
	}

	parentIntf, parentErr := pb.parentBlock()
	if parentErr != nil {
		return parentErr
	}

	// The parent of a proposal block (ie this block) must be a decision block
	parent, ok := parentIntf.(decision)
	if !ok {
		return errInvalidBlockType
	}

	// parentState is the state if this block's parent is accepted
	parentState := parent.onAccept()

	var err error
	pb.onCommitState, pb.onAbortState, err = tx.Execute(pb.vm, parentState, &pb.Tx)
	if err != nil {
		txID := tx.ID()
		pb.vm.droppedTxCache.Put(txID, err.Error()) // cache tx as dropped
		return err
	}
	pb.onCommitState.AddTx(&pb.Tx, status.Committed)
	pb.onAbortState.AddTx(&pb.Tx, status.Aborted)

	pb.timestamp = parentState.GetTimestamp()

	pb.vm.blockBuilder.RemoveProposalTx(&pb.Tx)
	pb.vm.currentBlocks[blkID] = pb
	parentIntf.addChild(pb)
	return nil
}

// Options returns the possible children of this block in preferential order.
func (pb *ProposalBlock) Options() ([2]snowman.Block, error) {
	tx, ok := pb.Tx.UnsignedTx.(UnsignedProposalTx)
	if !ok {
		return [2]snowman.Block{}, fmt.Errorf(
			"%w, expected UnsignedProposalTx but got %T",
			errWrongTxType,
			pb.Tx.UnsignedTx,
		)
	}

	blkID := pb.ID()
	nextHeight := pb.Height() + 1
	prefersCommit := tx.InitiallyPrefersCommit(pb.vm)

	commit, err := pb.vm.newCommitBlock(blkID, nextHeight, prefersCommit)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}
	abort, err := pb.vm.newAbortBlock(blkID, nextHeight, !prefersCommit)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}

	if prefersCommit {
		return [2]snowman.Block{commit, abort}, nil
	}
	return [2]snowman.Block{abort, commit}, nil
}

// newProposalBlock creates a new block that proposes to issue a transaction.
//
// The parent of this block has ID [parentID].
//
// The parent must be a decision block.
func (vm *VM) newProposalBlock(parentID ids.ID, height uint64, tx Tx) (*ProposalBlock, error) {
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
	bytes, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return pb, pb.initialize(vm, bytes, choices.Processing, pb)
}
