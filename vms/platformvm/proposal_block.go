// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	_ Block = &ProposalBlock{}
)

// ProposalBlock is a proposal to change the chain's state.
// A proposal may be to:
// 	1. Advance the chain's timestamp (*AdvanceTimeTx)
//  2. Remove a staker from the staker set (*RewardStakerTx)
//  3. Add a new staker to the set of pending (future) stakers (*AddStakerTx)
// The proposal will be enacted (change the chain's state) if the proposal block
// is accepted and followed by an accepted Commit block
type ProposalBlock struct {
	CommonBlock `serialize:"true"`

	Tx Tx `serialize:"true" json:"tx"`

	// The state that the chain will have if this block's proposal is committed
	onCommitState versionedState
	// The state that the chain will have if this block's proposal is aborted
	onAbortState versionedState
	// The function to execute if this block's proposal is committed
	onCommitFunc func() error
	// The function to execute if this block's proposal is aborted
	onAbortFunc func() error
}

func (pb *ProposalBlock) free() {
	pb.CommonBlock.free()
	pb.onCommitState = nil
	pb.onAbortState = nil
}

// Accept implements the snowman.Block interface
func (pb *ProposalBlock) Accept() error {
	blkID := pb.ID()
	pb.vm.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		pb.Height(),
		pb.ParentID(),
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
		pb.ParentID(),
	)

	pb.onCommitState = nil
	pb.onAbortState = nil

	if err := pb.vm.mempool.IssueTx(&pb.Tx); err != nil {
		pb.vm.ctx.Log.Verbo("failed to reissue tx %q due to: %s", pb.Tx.ID(), err)
	}
	return pb.CommonBlock.Reject()
}

// Initialize this block.
// Sets [pb.vm] to [vm] and populates non-serialized fields
// This method should be called when a block is unmarshaled from bytes
func (pb *ProposalBlock) initialize(vm *VM, bytes []byte, status choices.Status, self Block) error {
	if err := pb.CommonBlock.initialize(vm, bytes, status, self); err != nil {
		return err
	}

	unsignedBytes, err := pb.vm.codec.Marshal(codecVersion, &pb.Tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("failed to marshal unsigned tx: %w", err)
	}
	signedBytes, err := pb.vm.codec.Marshal(codecVersion, &pb.Tx)
	if err != nil {
		return fmt.Errorf("failed to marshal tx: %w", err)
	}
	pb.Tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

// setBaseDatabase sets this block's base state
func (pb *ProposalBlock) setBaseState() {
	pb.onCommitState.SetBase(pb.vm.internalState)
	pb.onAbortState.SetBase(pb.vm.internalState)
}

// onCommit should only be called after Verify is called.
// onCommit returns:
//   1. The state of the chain assuming this proposal is enacted. (That is, if
//      this block is accepted and followed by an accepted Commit block.)
//   2. A function be be executed when this block's proposal is committed. This
//      function should not write to state.
func (pb *ProposalBlock) onCommit() (versionedState, func() error) {
	return pb.onCommitState, pb.onCommitFunc
}

// onAbort should only be called after Verify is called.
// onAbort returns:
//   1. The state of the chain assuming this proposal is not enacted. (That is,
//      if this block is accepted and followed by an accepted Abort block.)
//   2. A function be be executed when this block's proposal is aborted. This
//      function should not write to state.
func (pb *ProposalBlock) onAbort() (versionedState, func() error) {
	return pb.onAbortState, pb.onAbortFunc
}

// Verify this block is valid.
//
// The parent block must either be a Commit or an Abort block.
//
// If this block is valid, this function also sets pas.onCommit and pas.onAbort.
func (pb *ProposalBlock) Verify() error {
	if err := pb.CommonBlock.Verify(); err != nil {
		if err := pb.Reject(); err != nil {
			pb.vm.ctx.Log.Error("failed to reject proposal block %s due to %s", pb.ID(), err)
		}
		return err
	}

	tx, ok := pb.Tx.UnsignedTx.(UnsignedProposalTx)
	if !ok {
		return errWrongTxType
	}

	parentIntf, parentErr := pb.parent()
	if parentErr != nil {
		return parentErr
	}

	// The parent of a proposal block (ie this block) must be a decision block
	parent, ok := parentIntf.(decision)
	if !ok {
		if err := pb.Reject(); err != nil {
			pb.vm.ctx.Log.Error("failed to reject proposal block %s due to %s", pb.ID(), err)
		}
		return errInvalidBlockType
	}

	// parentState is the state if this block's parent is accepted
	parentState := parent.onAccept()

	var err TxError
	pb.onCommitState, pb.onAbortState, pb.onCommitFunc, pb.onAbortFunc, err = tx.SemanticVerify(pb.vm, parentState, &pb.Tx)
	if err != nil {
		txID := tx.ID()
		pb.vm.droppedTxCache.Put(txID, err.Error()) // cache tx as dropped
		// If this block's transaction proposes to advance the timestamp, the transaction may fail
		// verification now but be valid in the future, so don't (permanently) mark the block as rejected.
		if !err.Temporary() {
			if err := pb.Reject(); err != nil {
				pb.vm.ctx.Log.Error("failed to reject proposal block %s due to %s", pb.ID(), err)
			}
		}
		return err
	}
	pb.onCommitState.AddTx(&pb.Tx, Committed)
	pb.onAbortState.AddTx(&pb.Tx, Aborted)

	pb.vm.currentBlocks[pb.ID()] = pb
	parentIntf.addChild(pb)
	return nil
}

// Options returns the possible children of this block in preferential order.
func (pb *ProposalBlock) Options() ([2]snowman.Block, error) {
	var (
		blockID    = pb.ID()
		nextHeight = pb.Height() + 1
		commit     Block
		abort      Block
	)

	commit, err := pb.vm.newCommitBlock(blockID, nextHeight)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}
	abort, err = pb.vm.newAbortBlock(blockID, nextHeight)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}

	pb.vm.internalState.AddBlock(commit)
	pb.vm.internalState.AddBlock(abort)

	if err := pb.vm.internalState.Commit(); err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to commit VM's database: %w",
			err,
		)
	}

	tx, ok := pb.Tx.UnsignedTx.(UnsignedProposalTx)
	if !ok {
		return [2]snowman.Block{}, errWrongTxType
	}

	if tx.InitiallyPrefersCommit(pb.vm) {
		return [2]snowman.Block{commit, abort}, nil
	}
	return [2]snowman.Block{abort, commit}, nil
}

// newProposalBlock creates a new block that proposes to issue a transaction.
// The parent of this block has ID [parentID]. The parent must be a decision block.
// Returns nil if there's an error while creating this block
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
	bytes, err := Codec.Marshal(codecVersion, &block)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return pb, pb.initialize(vm, bytes, choices.Processing, pb)
}
