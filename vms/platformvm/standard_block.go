// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	errConflictingBatchTxs = errors.New("block contains conflicting transactions")

	_ Block = &StandardBlock{}
)

// StandardBlock being accepted results in the transactions contained in the
// block to be accepted and committed to the chain.
type StandardBlock struct {
	CommonDecisionBlock `serialize:"true"`

	Txs []*txs.Tx `serialize:"true" json:"txs"`

	// inputs are the atomic inputs that are consumed by this block's atomic
	// transactions
	inputs ids.Set

	atomicRequests map[ids.ID]*atomic.Requests
}

func (sb *StandardBlock) initialize(vm *VM, bytes []byte, status choices.Status, blk Block) error {
	if err := sb.CommonDecisionBlock.initialize(vm, bytes, status, blk); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
		tx.Unsigned.InitCtx(vm.ctx)
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
// The parent block must be a decision block
//
// This function also sets onAcceptDB database if the verification passes.
func (sb *StandardBlock) Verify() error {
	blkID := sb.ID()

	if err := sb.CommonDecisionBlock.Verify(); err != nil {
		return err
	}

	onAcceptState, err := state.NewDiff(
		sb.PrntID,
		sb.vm.stateVersions,
	)
	if err != nil {
		return err
	}

	// clear inputs so that multiple [Verify] calls can be made
	sb.inputs.Clear()
	sb.atomicRequests = make(map[ids.ID]*atomic.Requests)

	funcs := make([]func(), 0, len(sb.Txs))
	for _, tx := range sb.Txs {
		txExecutor := executor.StandardTxExecutor{
			Backend: &sb.vm.txExecutorBackend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err := tx.Unsigned.Visit(&txExecutor)
		if err != nil {
			txID := tx.ID()
			sb.vm.blockBuilder.MarkDropped(txID, err.Error()) // cache tx as dropped
			return err
		}

		if sb.inputs.Overlaps(txExecutor.Inputs) {
			return errConflictingBatchTxs
		}
		sb.inputs.Union(txExecutor.Inputs)

		onAcceptState.AddTx(tx, status.Committed)
		if txExecutor.OnAccept != nil {
			funcs = append(funcs, txExecutor.OnAccept)
		}

		for chainID, txRequests := range txExecutor.AtomicRequests {
			// Add/merge in the atomic requests represented by [tx]
			chainRequests, exists := sb.atomicRequests[chainID]
			if !exists {
				sb.atomicRequests[chainID] = txRequests
				continue
			}

			chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
			chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
		}
	}

	if sb.inputs.Len() > 0 {
		parent, err := sb.parentBlock()
		if err != nil {
			return err
		}

		// ensure it doesn't conflict with the parent block
		conflicts, err := parent.conflicts(sb.inputs)
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
		sb.onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	sb.onAcceptState = onAcceptState
	sb.timestamp = onAcceptState.GetTimestamp()

	sb.vm.blockBuilder.RemoveDecisionTxs(sb.Txs)
	sb.vm.currentBlocks[blkID] = sb
	sb.vm.stateVersions.SetState(blkID, onAcceptState)
	return nil
}

func (sb *StandardBlock) Accept() error {
	blkID := sb.ID()
	sb.vm.ctx.Log.Verbo("accepting block with ID %s", blkID)

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

	if err := sb.vm.ctx.SharedMemory.Apply(sb.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	if sb.onAcceptFunc != nil {
		sb.onAcceptFunc()
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
func (vm *VM) newStandardBlock(parentID ids.ID, height uint64, txSlice []*txs.Tx) (*StandardBlock, error) {
	sb := &StandardBlock{
		CommonDecisionBlock: CommonDecisionBlock{
			CommonBlock: CommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
		},
		Txs: txSlice,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(sb)
	bytes, err := Codec.Marshal(txs.Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return sb, sb.initialize(vm, bytes, choices.Processing, sb)
}
