// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

var (
	errConflictingBatchTxs = errors.New("block contains conflicting transactions")

	_ Block    = &StandardBlock{}
	_ decision = &StandardBlock{}
)

// StandardBlock being accepted results in the transactions contained in the
// block to be accepted and committed to the chain.
type StandardBlock struct {
	CommonDecisionBlock `serialize:"true"`

	Txs []*Tx `serialize:"true" json:"txs"`

	// inputs are the atomic inputs that are consumed by this block's atomic
	// transactions
	inputs ids.Set
}

func (sb *StandardBlock) initialize(vm *VM, bytes []byte, status choices.Status, blk Block) error {
	if err := sb.CommonDecisionBlock.initialize(vm, bytes, status, blk); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
		tx.InitCtx(vm.ctx)
	}
	return nil
}

// conflicts checks to see if the provided input set contains any conflicts with
// any of this block's non-accepted ancestors or itself.
func (sb *StandardBlock) conflicts(s ids.Set) (bool, error) {
	if sb.Status() == choices.Accepted {
		return false, nil
	}
	if sb.inputs.Overlaps(s) {
		return true, nil
	}
	parent, err := sb.parentBlock()
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
func (sb *StandardBlock) Verify() error {
	blkID := sb.ID()

	if err := sb.CommonDecisionBlock.Verify(); err != nil {
		return err
	}

	parentIntf, err := sb.parentBlock()
	if err != nil {
		return err
	}

	// StandardBlock is not a modifier on a proposal block, so its parent must
	// be a decision.
	parent, ok := parentIntf.(decision)
	if !ok {
		return errInvalidBlockType
	}

	parentState := parent.onAccept()
	sb.onAcceptState = newVersionedState(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)

	// clear inputs so that multiple [Verify] calls can be made
	sb.inputs.Clear()

	funcs := make([]func() error, 0, len(sb.Txs))
	for _, tx := range sb.Txs {
		txID := tx.ID()

		utx, ok := tx.UnsignedTx.(UnsignedDecisionTx)
		if !ok {
			return errWrongTxType
		}

		inputUTXOs := utx.InputUTXOs()
		// ensure it doesn't overlap with current input batch
		if sb.inputs.Overlaps(inputUTXOs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		sb.inputs.Union(inputUTXOs)

		onAccept, err := utx.Execute(sb.vm, sb.onAcceptState, tx)
		if err != nil {
			sb.vm.droppedTxCache.Put(txID, err.Error()) // cache tx as dropped
			return err
		}

		sb.onAcceptState.AddTx(tx, status.Committed)
		if onAccept != nil {
			funcs = append(funcs, onAccept)
		}
	}

	if sb.inputs.Len() > 0 {
		// ensure it doesnt conflict with the parent block
		conflicts, err := parentIntf.conflicts(sb.inputs)
		if err != nil {
			return err
		}
		if conflicts {
			return errConflictingParentTxs
		}
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		sb.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		sb.onAcceptFunc = func() error {
			for _, f := range funcs {
				if err := f(); err != nil {
					return fmt.Errorf("failed to execute onAcceptFunc: %w", err)
				}
			}
			return nil
		}
	}

	sb.timestamp = sb.onAcceptState.GetTimestamp()

	sb.vm.blockBuilder.RemoveDecisionTxs(sb.Txs)
	sb.vm.currentBlocks[blkID] = sb
	parentIntf.addChild(sb)
	return nil
}

func (sb *StandardBlock) Accept() error {
	blkID := sb.ID()
	sb.vm.ctx.Log.Verbo("accepting block with ID %s", blkID)

	// Set up the shared memory operations
	sharedMemoryOps := make(map[ids.ID]*atomic.Requests)
	for _, tx := range sb.Txs {
		utx, ok := tx.UnsignedTx.(UnsignedDecisionTx)
		if !ok {
			return errWrongTxType
		}

		// Get the shared memory operations this transaction is performing
		chainID, txRequests, err := utx.AtomicOperations()
		if err != nil {
			return err
		}

		// Only [AtomicTx]s will return operations to be applied to shared memory
		if txRequests == nil {
			continue
		}

		// Add/merge in the atomic requests represented by [tx]
		chainRequests, exists := sharedMemoryOps[chainID]
		if !exists {
			sharedMemoryOps[chainID] = txRequests
			continue
		}

		chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
		chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
	}

	if err := sb.CommonDecisionBlock.Accept(); err != nil {
		return fmt.Errorf("failed to accept CommonDecisionBlock: %w", err)
	}

	// Update the state of the chain in the database
	sb.onAcceptState.Apply(sb.vm.internalState)

	defer sb.vm.internalState.Abort()
	batch, err := sb.vm.internalState.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := sb.vm.ctx.SharedMemory.Apply(sharedMemoryOps, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	for _, child := range sb.children {
		child.setBaseState()
	}
	if sb.onAcceptFunc != nil {
		if err := sb.onAcceptFunc(); err != nil {
			return fmt.Errorf("failed to execute onAcceptFunc: %w", err)
		}
	}

	sb.free()
	return nil
}

func (sb *StandardBlock) Reject() error {
	sb.vm.ctx.Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		sb.ID(),
		sb.Height(),
		sb.Parent(),
	)

	for _, tx := range sb.Txs {
		if err := sb.vm.blockBuilder.AddVerifiedTx(tx); err != nil {
			sb.vm.ctx.Log.Debug(
				"failed to reissue tx %q due to: %s",
				tx.ID(),
				err,
			)
		}
	}
	return sb.CommonDecisionBlock.Reject()
}

// newStandardBlock returns a new *StandardBlock where the block's parent, a
// decision block, has ID [parentID].
func (vm *VM) newStandardBlock(parentID ids.ID, height uint64, txs []*Tx) (*StandardBlock, error) {
	sb := &StandardBlock{
		CommonDecisionBlock: CommonDecisionBlock{
			CommonBlock: CommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
		},
		Txs: txs,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(sb)
	bytes, err := Codec.Marshal(CodecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return sb, sb.initialize(vm, bytes, choices.Processing, sb)
}
