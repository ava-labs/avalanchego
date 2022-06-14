// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
)

var (
	errConflictingBatchTxs = errors.New("block contains conflicting transactions")

	_ Block    = &StandardBlock{}
	_ Decision = &StandardBlock{}
)

// StandardBlock being accepted results in the transactions contained in the
// block to be accepted and committed to the chain.
type StandardBlock struct {
	stateless.StandardBlockIntf
	*decisionBlock

	// inputs are the atomic inputs that are consumed by this block's atomic
	// transactions
	inputs ids.Set
}

// NewStandardBlock returns a new *StandardBlock where the block's parent, a
// decision block, has ID [parentID].
func NewStandardBlock(
	version uint16,
	verifier Verifier,
	parentID ids.ID,
	height uint64,
	txs []*signed.Tx,
) (*StandardBlock, error) {
	statelessBlk, err := stateless.NewStandardBlock(version, parentID, height, txs)
	if err != nil {
		return nil, err
	}
	return toStatefulStandardBlock(statelessBlk, verifier, choices.Processing)
}

func toStatefulStandardBlock(
	statelessBlk stateless.StandardBlockIntf,
	verifier Verifier,
	status choices.Status,
) (*StandardBlock, error) {
	sb := &StandardBlock{
		StandardBlockIntf: statelessBlk,
		decisionBlock: &decisionBlock{
			commonBlock: &commonBlock{
				commonStatelessBlk: statelessBlk,
				status:             status,
				verifier:           verifier,
			},
		},
	}

	for _, tx := range sb.DecisionTxs() {
		tx.Unsigned.InitCtx(sb.verifier.Ctx())
	}

	return sb, nil
}

// conflicts checks to see if the provided input set contains any conflicts with
// any of this block's non-accepted ancestors or itself.
func (sb *StandardBlock) conflicts(s ids.Set) (bool, error) {
	if sb.status == choices.Accepted {
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
	if err := sb.verify(); err != nil {
		return err
	}

	parentIntf, err := sb.parentBlock()
	if err != nil {
		return err
	}

	// StandardBlock is not a modifier on a proposal block, so its parent must
	// be a decision.
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}

	parentState := parent.OnAccept()
	sb.onAcceptState = state.NewVersioned(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)

	// clear inputs so that multiple [Verify] calls can be made
	sb.inputs.Clear()

	txs := sb.DecisionTxs()
	funcs := make([]func() error, 0, len(txs))
	for _, tx := range txs {
		txID := tx.ID()

		inputUTXOs, err := sb.verifier.InputUTXOs(tx.Unsigned)
		if err != nil {
			return err
		}
		// ensure it doesn't overlap with current input batch
		if sb.inputs.Overlaps(inputUTXOs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		sb.inputs.Union(inputUTXOs)

		onAccept, err := sb.verifier.ExecuteDecision(tx, sb.onAcceptState)
		if err != nil {
			sb.verifier.MarkDropped(txID, err.Error()) // cache tx as dropped
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
			return ErrConflictingParentTxs
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

	sb.SetTimestamp(sb.onAcceptState.GetTimestamp())

	sb.verifier.RemoveDecisionTxs(txs)
	sb.verifier.CacheVerifiedBlock(sb)
	parentIntf.addChild(sb)
	return nil
}

func (sb *StandardBlock) Accept() error {
	blkID := sb.ID()
	sb.verifier.Ctx().Log.Verbo("accepting block with ID %s", blkID)

	// Set up the shared memory operations
	txs := sb.DecisionTxs()
	sharedMemoryOps := make(map[ids.ID]*atomic.Requests)
	for _, tx := range txs {
		// Get the shared memory operations this transaction is performing
		chainID, txRequests, err := sb.verifier.AtomicOperations(tx)
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

	sb.accept()
	sb.verifier.AddStatelessBlock(sb.StandardBlockIntf, sb.Status())
	if err := sb.verifier.RegisterBlock(sb.StandardBlockIntf); err != nil {
		return fmt.Errorf("failed to accept standard block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	sb.onAcceptState.Apply(sb.verifier)

	defer sb.verifier.Abort()
	batch, err := sb.verifier.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := sb.verifier.Ctx().SharedMemory.Apply(sharedMemoryOps, batch); err != nil {
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
	sb.verifier.Ctx().Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		sb.ID(),
		sb.Height(),
		sb.Parent(),
	)

	txs := sb.DecisionTxs()
	for _, tx := range txs {
		if err := sb.verifier.Add(tx); err != nil {
			sb.verifier.Ctx().Log.Debug(
				"failed to reissue tx %q due to: %s",
				tx.ID(),
				err,
			)
		}
	}

	defer sb.reject()
	sb.verifier.AddStatelessBlock(sb.StandardBlockIntf, sb.Status())
	return sb.verifier.Commit()
}
