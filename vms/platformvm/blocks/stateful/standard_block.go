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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
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

	// Inputs are the atomic Inputs that are consumed by this block's atomic
	// transactions
	Inputs ids.Set

	atomicRequests map[ids.ID]*atomic.Requests
}

// NewStandardBlock returns a new *StandardBlock where the block's parent, a
// decision block, has ID [parentID].
func NewStandardBlock(
	version uint16,
	timestamp uint64,
	verifier Verifier,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	txs []*txs.Tx,
) (*StandardBlock, error) {
	statelessBlk, err := stateless.NewStandardBlock(version, timestamp, parentID, height, txs)
	if err != nil {
		return nil, err
	}
	return toStatefulStandardBlock(statelessBlk, verifier, txExecutorBackend, choices.Processing)
}

func toStatefulStandardBlock(
	statelessBlk stateless.StandardBlockIntf,
	verifier Verifier,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (*StandardBlock, error) {
	sb := &StandardBlock{
		StandardBlockIntf: statelessBlk,
		decisionBlock: &decisionBlock{
			commonBlock: &commonBlock{
				commonStatelessBlk: statelessBlk,
				status:             status,
				verifier:           verifier,
				txExecutorBackend:  txExecutorBackend,
			},
		},
	}

	for _, tx := range sb.DecisionTxs() {
		tx.Unsigned.InitCtx(sb.txExecutorBackend.Ctx)
	}

	return sb, nil
}

// conflicts checks to see if the provided input set contains any conflicts with
// any of this block's non-accepted ancestors or itself.
func (sb *StandardBlock) conflicts(s ids.Set) (bool, error) {
	if sb.status == choices.Accepted {
		return false, nil
	}
	if sb.Inputs.Overlaps(s) {
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
	blkVersion := sb.Version()
	switch blkVersion {
	case stateless.PreForkVersion:
		sb.onAcceptState = state.NewVersioned(
			parentState,
			parentState.CurrentStakerChainState(),
			parentState.PendingStakerChainState(),
		)

	case stateless.PostForkVersion:
		// We update staker set before processing block transactions
		nextChainTime := sb.Timestamp()
		currentStakers := parentState.CurrentStakerChainState()
		pendingStakers := parentState.PendingStakerChainState()
		currentSupply := parentState.GetCurrentSupply()
		newlyCurrentStakers,
			newlyPendingStakers,
			updatedSupply,
			err := executor.UpdateStakerSet(
			currentStakers,
			pendingStakers,
			currentSupply,
			&sb.txExecutorBackend,
			nextChainTime,
		)
		if err != nil {
			return err
		}

		sb.onAcceptState = state.NewVersioned(
			parentState,
			newlyCurrentStakers,
			newlyPendingStakers,
		)
		sb.onAcceptState.SetTimestamp(nextChainTime)
		sb.onAcceptState.SetCurrentSupply(updatedSupply)

	default:
		return fmt.Errorf(
			"block version %d, unknown recipe to update chain state. Verification failed",
			blkVersion,
		)
	}

	// clear inputs so that multiple [Verify] calls can be made
	sb.Inputs.Clear()
	sb.atomicRequests = make(map[ids.ID]*atomic.Requests)

	txs := sb.DecisionTxs()
	funcs := make([]func() error, 0, len(txs))
	for _, tx := range txs {
		txExecutor := executor.StandardTxExecutor{
			Backend: &sb.txExecutorBackend,
			State:   sb.onAcceptState,
			Tx:      tx,
		}
		err := tx.Unsigned.Visit(&txExecutor)
		if err != nil {
			txID := tx.ID()
			sb.verifier.MarkDropped(txID, err.Error()) // cache tx as dropped
			return err
		}
		// ensure it doesn't overlap with current input batch
		if sb.Inputs.Overlaps(txExecutor.Inputs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		sb.Inputs.Union(txExecutor.Inputs)

		sb.onAcceptState.AddTx(tx, status.Committed)
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

	if sb.Inputs.Len() > 0 {
		// ensure it doesnt conflict with the parent block
		conflicts, err := parentIntf.conflicts(sb.Inputs)
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
	sb.txExecutorBackend.Ctx.Log.Verbo("accepting block with ID %s", blkID)

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

	if err := sb.txExecutorBackend.Ctx.SharedMemory.Apply(sb.atomicRequests, batch); err != nil {
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
	sb.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		sb.ID(),
		sb.Height(),
		sb.Parent(),
	)

	txs := sb.DecisionTxs()
	for _, tx := range txs {
		if err := sb.verifier.Add(tx); err != nil {
			sb.txExecutorBackend.Ctx.Log.Debug(
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
