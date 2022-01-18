// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

var (
	errConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")

	_ Block    = &AtomicBlock{}
	_ decision = &AtomicBlock{}
)

// AtomicBlock being accepted results in the atomic transaction contained in the
// block to be accepted and committed to the chain.
type AtomicBlock struct {
	CommonDecisionBlock `serialize:"true"`

	Tx Tx `serialize:"true" json:"tx"`

	// inputs are the atomic inputs that are consumed by this block's atomic
	// transaction
	inputs ids.Set
}

func (ab *AtomicBlock) initialize(vm *VM, bytes []byte, status choices.Status, self Block) error {
	if err := ab.CommonDecisionBlock.initialize(vm, bytes, status, self); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	unsignedBytes, err := Codec.Marshal(CodecVersion, &ab.Tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("failed to marshal unsigned tx: %w", err)
	}
	signedBytes, err := Codec.Marshal(CodecVersion, &ab.Tx)
	if err != nil {
		return fmt.Errorf("failed to marshal tx: %w", err)
	}
	ab.Tx.Initialize(unsignedBytes, signedBytes)
	ab.Tx.InitCtx(vm.ctx)
	return nil
}

// conflicts checks to see if the provided input set contains any conflicts with
// any of this block's non-accepted ancestors or itself.
func (ab *AtomicBlock) conflicts(s ids.Set) (bool, error) {
	if ab.Status() == choices.Accepted {
		return false, nil
	}
	if ab.inputs.Overlaps(s) {
		return true, nil
	}
	parent, err := ab.parentBlock()
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

// Verify this block performs a valid state transition.
//
// The parent block must be a decision block
//
// This function also sets onAcceptDB database if the verification passes.
func (ab *AtomicBlock) Verify() error {
	blkID := ab.ID()

	if err := ab.CommonDecisionBlock.Verify(); err != nil {
		return err
	}

	tx, ok := ab.Tx.UnsignedTx.(UnsignedAtomicTx)
	if !ok {
		return errWrongTxType
	}
	ab.inputs = tx.InputUTXOs()

	parentIntf, err := ab.parentBlock()
	if err != nil {
		return err
	}

	conflicts, err := parentIntf.conflicts(ab.inputs)
	if err != nil {
		return err
	}
	if conflicts {
		return errConflictingParentTxs
	}

	// AtomicBlock is not a modifier on a proposal block, so its parent must be
	// a decision.
	parent, ok := parentIntf.(decision)
	if !ok {
		return errInvalidBlockType
	}

	parentState := parent.onAccept()

	currentTimestamp := parentState.GetTimestamp()
	enabledAP5 := !currentTimestamp.Before(ab.vm.ApricotPhase5Time)

	if enabledAP5 {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			ab.vm.ApricotPhase5Time.Unix(),
		)
	}

	onAccept, err := tx.AtomicExecute(ab.vm, parentState, &ab.Tx)
	if err != nil {
		txID := tx.ID()
		ab.vm.droppedTxCache.Put(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}
	onAccept.AddTx(&ab.Tx, status.Committed)

	ab.onAcceptState = onAccept
	ab.timestamp = onAccept.GetTimestamp()

	ab.vm.blockBuilder.RemoveDecisionTxs([]*Tx{&ab.Tx})
	ab.vm.currentBlocks[blkID] = ab
	parentIntf.addChild(ab)
	return nil
}

func (ab *AtomicBlock) Accept() error {
	blkID := ab.ID()
	ab.vm.ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		ab.Height(),
		ab.Parent(),
	)

	if err := ab.CommonBlock.Accept(); err != nil {
		return fmt.Errorf("failed to accept CommonBlock of %s: %w", blkID, err)
	}

	tx, ok := ab.Tx.UnsignedTx.(UnsignedAtomicTx)
	if !ok {
		return errWrongTxType
	}

	// Update the state of the chain in the database
	ab.onAcceptState.Apply(ab.vm.internalState)

	defer ab.vm.internalState.Abort()
	batch, err := ab.vm.internalState.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := tx.AtomicAccept(ab.vm.ctx, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			tx.ID(),
			blkID,
			err,
		)
	}

	for _, child := range ab.children {
		child.setBaseState()
	}
	if ab.onAcceptFunc != nil {
		if err := ab.onAcceptFunc(); err != nil {
			return fmt.Errorf(
				"failed to execute onAcceptFunc of %s: %w",
				blkID,
				err,
			)
		}
	}

	ab.free()
	return nil
}

func (ab *AtomicBlock) Reject() error {
	ab.vm.ctx.Log.Verbo(
		"Rejecting Atomic Block %s at height %d with parent %s",
		ab.ID(),
		ab.Height(),
		ab.Parent(),
	)

	if err := ab.vm.blockBuilder.AddVerifiedTx(&ab.Tx); err != nil {
		ab.vm.ctx.Log.Debug(
			"failed to reissue tx %q due to: %s",
			ab.Tx.ID(),
			err,
		)
	}
	return ab.CommonDecisionBlock.Reject()
}

// newAtomicBlock returns a new *AtomicBlock where the block's parent, a
// decision block, has ID [parentID].
func (vm *VM) newAtomicBlock(parentID ids.ID, height uint64, tx Tx) (*AtomicBlock, error) {
	ab := &AtomicBlock{
		CommonDecisionBlock: CommonDecisionBlock{
			CommonBlock: CommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
		},
		Tx: tx,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(ab)
	bytes, err := Codec.Marshal(CodecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return ab, ab.initialize(vm, bytes, choices.Processing, ab)
}
