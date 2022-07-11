// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_                       stateless.Visitor = &verifier{}
	errConflictingBatchTxs                    = errors.New("block contains conflicting transactions")
	ErrConflictingParentTxs                   = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
)

type verifier struct {
	backend
	txExecutorBackend executor.Backend
}

func (v *verifier) VisitProposalBlock(b *stateless.ProposalBlock) error {
	blkState, ok := v.blkIDToState[b.ID()]
	if !ok {
		blkState = &blockState{
			statelessBlock: b,
		}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState := v.OnAccept(parentID)

	txExecutor := executor.ProposalTxExecutor{
		Backend:     &v.txExecutorBackend,
		ParentState: parentState,
		Tx:          b.Tx,
	}
	if err := b.Tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	// blkID := b.ID()

	onCommitState := txExecutor.OnCommit
	onCommitState.AddTx(b.Tx, status.Committed)
	// v.blkIDToOnCommitState[blkID] = onCommitState
	blkState.onCommitState = onCommitState

	onAbortState := txExecutor.OnAbort
	onAbortState.AddTx(b.Tx, status.Aborted)
	// v.blkIDToOnAbortState[blkID] = onAbortState
	blkState.onAbortState = onAbortState

	// v.blkIDToTimestamp[blkID] = parentState.GetTimestamp()
	blkState.timestamp = parentState.GetTimestamp()
	// TODO
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	// v.blkIDToPreferCommit[blkID] = txExecutor.PrefersCommit
	blkState.inititallyPreferCommit = txExecutor.PrefersCommit

	v.Mempool.RemoveProposalTx(b.Tx)
	// v.pinVerifiedBlock(b)
	v.blkIDToState[b.ID()] = blkState
	return nil
}

func (v *verifier) VisitAtomicBlock(b *stateless.AtomicBlock) error {
	blkState, ok := v.blkIDToState[b.ID()]
	if !ok {
		blkState = &blockState{
			statelessBlock: b,
		}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// parentIntf, err := v.parent(b.baseBlk)
	// if err != nil {
	// 	return err
	// }
	// parentIntf, err := v.GetStatefulBlock(b.Parent())
	// if err != nil {
	// 	return err
	// }

	parentState := v.OnAccept(b.Parent())

	cfg := v.txExecutorBackend.Cfg
	currentTimestamp := parentState.GetTimestamp()
	enbledAP5 := !currentTimestamp.Before(cfg.ApricotPhase5Time)

	if enbledAP5 {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			cfg.ApricotPhase5Time.Unix(),
		)
	}

	atomicExecutor := executor.AtomicTxExecutor{
		Backend:     &v.txExecutorBackend,
		ParentState: parentState,
		Tx:          b.Tx,
	}
	if err := b.Tx.Unsigned.Visit(&atomicExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(b.Tx, status.Committed)

	blkID := b.ID()
	// v.blkIDToOnAcceptState[blkID] = atomicExecutor.OnAccept
	blkState.onAcceptState = atomicExecutor.OnAccept
	// v.blkIDToInputs[blkID] = atomicExecutor.Inputs
	blkState.inputs = atomicExecutor.Inputs
	// v.blkIDToAtomicRequests[blkID] = atomicExecutor.AtomicRequests
	blkState.atomicRequests = atomicExecutor.AtomicRequests
	// v.blkIDToTimestamp[blkID] = atomicExecutor.OnAccept.GetTimestamp()
	blkState.timestamp = atomicExecutor.OnAccept.GetTimestamp()

	// Check for conflicts in atomic inputs
	var nextBlock stateless.Block = b
	for {
		parentID := nextBlock.Parent()
		parentState := v.blkIDToState[parentID]
		if parentState == nil {
			// The parent state isn't pinned in memory.
			// This means the parent must be accepted already.
			break
		}
		if parentState.inputs.Overlaps(atomicExecutor.Inputs) {
			return ErrConflictingParentTxs
		}
		parent, _, err := v.GetStatelessBlock(parentID)
		if err != nil {
			return err
		}
		nextBlock = parent
	}

	// conflicts, err := parentIntf.conflicts(atomicExecutor.Inputs)
	// if err != nil {
	// 	return err
	// }
	// if conflicts {
	// 	return ErrConflictingParentTxs
	// }

	v.Mempool.RemoveDecisionTxs([]*txs.Tx{b.Tx})
	// TODO
	// parentID := b.Parent()
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)

	// v.pinVerifiedBlock(b)
	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) VisitStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	blkState, ok := v.blkIDToState[blkID]
	if !ok {
		blkState = &blockState{
			statelessBlock: b,
		}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// parentIntf, err := v.parent(b.baseBlk)
	// if err != nil {
	// 	return err
	// }
	// parentIntf, err := v.GetStatefulBlock(b.Parent())
	// if err != nil {
	// 	return err
	// }

	parentState := v.OnAccept(b.Parent())

	onAcceptState := state.NewDiff(
		parentState,
		parentState.CurrentStakers(),
		parentState.PendingStakers(),
	)

	// TODO do we still need to do something similar to the below?
	// clear inputs so that multiple [Verify] calls can be made
	// b.Inputs.Clear()
	// b.atomicRequests = make(map[ids.ID]*atomic.Requests)

	funcs := make([]func(), 0, len(b.Txs))
	// blockInputs, ok := v.blkIDToInputs[blkID]
	// if !ok {
	// 	blockInputs = ids.Set{}
	// 	v.blkIDToInputs[blkID] = blockInputs
	// }
	// atomicRequests := v.blkIDToAtomicRequests[blkID]
	// if !ok {
	// 	atomicRequests = make(map[ids.ID]*atomic.Requests)
	// 	v.blkIDToAtomicRequests[blkID] = atomicRequests
	// }
	for _, tx := range b.Txs {
		txExecutor := executor.StandardTxExecutor{
			Backend: &v.txExecutorBackend,
			State:   onAcceptState,
			Tx:      tx,
		}
		if err := tx.Unsigned.Visit(&txExecutor); err != nil {
			txID := tx.ID()
			v.MarkDropped(txID, err.Error()) // cache tx as dropped
			return err
		}
		// ensure it doesn't overlap with current input batch
		if blkState.inputs.Overlaps(txExecutor.Inputs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		blkState.inputs.Union(txExecutor.Inputs)

		onAcceptState.AddTx(tx, status.Committed)
		if txExecutor.OnAccept != nil {
			funcs = append(funcs, txExecutor.OnAccept)
		}

		for chainID, txRequests := range txExecutor.AtomicRequests {
			// Add/merge in the atomic requests represented by [tx]
			chainRequests, exists := blkState.atomicRequests[chainID]
			if !exists {
				blkState.atomicRequests[chainID] = txRequests
				continue
			}

			chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
			chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
		}
	}

	if blkState.inputs.Len() > 0 {
		// ensure it doesnt conflict with the parent block
		var nextBlock stateless.Block = b
		for {
			parentID := nextBlock.Parent()
			parentState := v.blkIDToState[parentID]
			if parentState == nil {
				// The parent state isn't pinned in memory.
				// This means the parent must be accepted already.
				break
			}
			if parentState.inputs.Overlaps(blkState.inputs) {
				return ErrConflictingParentTxs
			}
			parent, _, err := v.GetStatelessBlock(parentID)
			if err != nil {
				return err
			}
			nextBlock = parent
		}
		// conflicts, err := parentIntf.conflicts(blkState.inputs)
		// if err != nil {
		// 	return err
		// }
		// if conflicts {
		// 	return ErrConflictingParentTxs
		// }
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		// v.blkIDToOnAcceptFunc[blkID] = funcs[0]
		blkState.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		// v.blkIDToOnAcceptFunc[blkID] = func() {
		// 	for _, f := range funcs {
		// 		f()
		// 	}
		// }
		blkState.onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	// v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	blkState.timestamp = onAcceptState.GetTimestamp()
	// v.blkIDToOnAcceptState[blkID] = onAcceptState
	blkState.onAcceptState = onAcceptState
	v.Mempool.RemoveDecisionTxs(b.Txs)
	// TODO
	// parentID := b.Parent()
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)

	// v.pinVerifiedBlock(b)
	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) VisitCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()
	blkState, ok := v.blkIDToState[blkID]
	if !ok {
		blkState = &blockState{
			statelessBlock: b,
		}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// TODO
	// parentID := b.Parent()
	// onAcceptState := v.blkIDToOnCommitState[parentID]
	onAcceptState := state.Diff(nil) // TODO get parent state
	// v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	blkState.timestamp = onAcceptState.GetTimestamp()
	// v.blkIDToOnAcceptState[blkID] = onAcceptState
	blkState.onAcceptState = onAcceptState

	// v.pinVerifiedBlock(b)
	v.blkIDToState[blkID] = blkState

	// TODO
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

func (v *verifier) VisitAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()
	blkState, ok := v.blkIDToState[blkID]
	if !ok {
		blkState = &blockState{
			statelessBlock: b,
		}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// parentID := b.Parent()
	// onAcceptState := v.blkIDToOnAbortState[parentID]
	onAcceptState := state.Diff(nil)
	// v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	blkState.timestamp = onAcceptState.GetTimestamp()
	// v.blkIDToOnAcceptState[blkID] = onAcceptState
	blkState.onAcceptState = onAcceptState

	// v.pinVerifiedBlock(b)

	// TODO
	// 	v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	v.blkIDToState[blkID] = blkState
	return nil
}

// Assumes [b] isn't nil
func (v *verifier) verifyCommonBlock(b stateless.CommonBlock) error {
	parent, _, err := v.GetStatelessBlock(b.Parent())
	if err != nil {
		return err
	}
	if expectedHeight := parent.Height() + 1; expectedHeight != b.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			b.Height(),
		)
	}
	return nil
}
