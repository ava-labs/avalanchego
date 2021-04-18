// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var (
	errConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
)

// AtomicBlock being accepted results in the transaction contained in the
// block to be accepted and committed to the chain.
type AtomicBlock struct {
	CommonDecisionBlock `serialize:"true"`

	Tx Tx `serialize:"true" json:"tx"`

	inputs ids.Set
}

// initialize this block
func (ab *AtomicBlock) initialize(vm *VM, bytes []byte, status choices.Status, self Block) error {
	if err := ab.CommonDecisionBlock.initialize(vm, bytes, status, self); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	unsignedBytes, err := vm.codec.Marshal(codecVersion, &ab.Tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("failed to marshal unsigned tx: %w", err)
	}
	signedBytes, err := ab.vm.codec.Marshal(codecVersion, &ab.Tx)
	if err != nil {
		return fmt.Errorf("failed to marshal tx: %w", err)
	}
	ab.Tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

// Reject implements the snowman.Block interface
func (ab *AtomicBlock) conflicts(s ids.Set) (bool, error) {
	if ab.Status() == choices.Accepted {
		return false, nil
	}
	if ab.inputs.Overlaps(s) {
		return true, nil
	}
	parent, err := ab.parent()
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptDB database if the verification passes.
func (ab *AtomicBlock) Verify() error {
	if err := ab.CommonDecisionBlock.Verify(); err != nil {
		if err := ab.Reject(); err != nil {
			ab.vm.ctx.Log.Error("failed to reject atomic block %s due to %s", ab.ID(), err)
		}
		return err
	}

	tx, ok := ab.Tx.UnsignedTx.(UnsignedAtomicTx)
	if !ok {
		return errWrongTxType
	}
	ab.inputs = tx.InputUTXOs()

	parentIntf, err := ab.parent()
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
	onAccept, err := tx.SemanticVerify(ab.vm, parentState, &ab.Tx)
	if err != nil {
		ab.vm.droppedTxCache.Put(ab.Tx.ID(), err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", tx.ID(), err)
	}
	onAccept.AddTx(&ab.Tx, Committed)

	ab.onAcceptState = onAccept
	ab.vm.currentBlocks[ab.ID()] = ab
	parentIntf.addChild(ab)
	return nil
}

// Accept implements the snowman.Block interface
func (ab *AtomicBlock) Accept() error {
	blkID := ab.ID()
	ab.vm.ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		ab.Height(),
		ab.ParentID(),
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
		return fmt.Errorf("failed to commit VM's database for block %s: %w", ab.ID(), err)
	}
	if err := tx.Accept(ab.vm.ctx, batch); err != nil {
		return fmt.Errorf("failed to atomically accept tx %s in block %s: %w", tx.ID(), ab.ID(), err)
	}

	for _, child := range ab.children {
		child.setBaseState()
	}
	if ab.onAcceptFunc != nil {
		if err := ab.onAcceptFunc(); err != nil {
			return fmt.Errorf("failed to execute onAcceptFunc of %s: %w", blkID, err)
		}
	}

	ab.free()
	return nil
}

// Reject implements the snowman.Block interface
func (ab *AtomicBlock) Reject() error {
	ab.vm.ctx.Log.Verbo("Rejecting Atomic Block %s at height %d with parent %s", ab.ID(), ab.Height(), ab.ParentID())

	if err := ab.vm.mempool.IssueTx(&ab.Tx); err != nil {
		ab.vm.ctx.Log.Debug("failed to reissue tx %q due to: %s", ab.Tx.ID(), err)
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
	bytes, err := Codec.Marshal(codecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return ab, ab.CommonDecisionBlock.initialize(vm, bytes, choices.Processing, ab)
}
