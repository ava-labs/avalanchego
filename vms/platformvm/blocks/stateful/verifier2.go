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
	_                       stateless.BlockVerifier = &verifier2{}
	errConflictingBatchTxs                          = errors.New("block contains conflicting transactions")
	ErrConflictingParentTxs                         = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
)

type verifier2 struct {
	backend
	txExecutorBackend executor.Backend
}

func (v *verifier2) VerifyProposalBlock(b *stateless.ProposalBlock) error {
	blockState, ok := v.blkIDToState[b.ID()]
	if !ok {
		blockState = &stat{}
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
	blockState.onCommitState = onCommitState

	onAbortState := txExecutor.OnAbort
	onAbortState.AddTx(b.Tx, status.Aborted)
	// v.blkIDToOnAbortState[blkID] = onAbortState
	blockState.onAbortState = onAbortState

	// v.blkIDToTimestamp[blkID] = parentState.GetTimestamp()
	blockState.timestamp = parentState.GetTimestamp()
	// TODO
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	// v.blkIDToPreferCommit[blkID] = txExecutor.PrefersCommit
	blockState.inititallyPreferCommit = txExecutor.PrefersCommit

	v.Mempool.RemoveProposalTx(b.Tx)
	// v.pinVerifiedBlock(b)
	v.blkIDToState[b.ID()] = blockState
	return nil
}

func (v *verifier2) VerifyAtomicBlock(b *stateless.AtomicBlock) error {
	blockState, ok := v.blkIDToState[b.ID()]
	if !ok {
		blockState = &stat{}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// parentIntf, err := v.parent(b.baseBlk)
	// if err != nil {
	// 	return err
	// }
	parentIntf, err := v.GetStatefulBlock(b.Parent())
	if err != nil {
		return err
	}

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
	blockState.onAcceptState = atomicExecutor.OnAccept
	// v.blkIDToInputs[blkID] = atomicExecutor.Inputs
	blockState.inputs = atomicExecutor.Inputs
	// v.blkIDToAtomicRequests[blkID] = atomicExecutor.AtomicRequests
	blockState.atomicRequests = atomicExecutor.AtomicRequests
	// v.blkIDToTimestamp[blkID] = atomicExecutor.OnAccept.GetTimestamp()
	blockState.timestamp = atomicExecutor.OnAccept.GetTimestamp()

	conflicts, err := parentIntf.conflicts(atomicExecutor.Inputs)
	if err != nil {
		return err
	}
	if conflicts {
		return ErrConflictingParentTxs
	}

	v.Mempool.RemoveDecisionTxs([]*txs.Tx{b.Tx})
	// TODO
	// parentID := b.Parent()
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)

	// v.pinVerifiedBlock(b)
	v.blkIDToState[blkID] = blockState
	return nil
}

func (v *verifier2) VerifyStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	blockState, ok := v.blkIDToState[blkID]
	if !ok {
		blockState = &stat{}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// parentIntf, err := v.parent(b.baseBlk)
	// if err != nil {
	// 	return err
	// }
	parentIntf, err := v.GetStatefulBlock(b.Parent())
	if err != nil {
		return err
	}

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
		if blockState.inputs.Overlaps(txExecutor.Inputs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		blockState.inputs.Union(txExecutor.Inputs)

		onAcceptState.AddTx(tx, status.Committed)
		if txExecutor.OnAccept != nil {
			funcs = append(funcs, txExecutor.OnAccept)
		}

		for chainID, txRequests := range txExecutor.AtomicRequests {
			// Add/merge in the atomic requests represented by [tx]
			chainRequests, exists := blockState.atomicRequests[chainID]
			if !exists {
				blockState.atomicRequests[chainID] = txRequests
				continue
			}

			chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
			chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
		}
	}

	if blockState.inputs.Len() > 0 {
		// ensure it doesnt conflict with the parent block
		conflicts, err := parentIntf.conflicts(blockState.inputs)
		if err != nil {
			return err
		}
		if conflicts {
			return ErrConflictingParentTxs
		}
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		// v.blkIDToOnAcceptFunc[blkID] = funcs[0]
		blockState.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		// v.blkIDToOnAcceptFunc[blkID] = func() {
		// 	for _, f := range funcs {
		// 		f()
		// 	}
		// }
		blockState.onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	// v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	blockState.timestamp = onAcceptState.GetTimestamp()
	// v.blkIDToOnAcceptState[blkID] = onAcceptState
	blockState.onAcceptState = onAcceptState
	v.Mempool.RemoveDecisionTxs(b.Txs)
	// TODO
	// parentID := b.Parent()
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)

	// v.pinVerifiedBlock(b)
	v.blkIDToState[blkID] = blockState
	return nil
}

func (v *verifier2) VerifyCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()
	blockState, ok := v.blkIDToState[blkID]
	if !ok {
		blockState = &stat{}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// TODO
	// parentID := b.Parent()
	// onAcceptState := v.blkIDToOnCommitState[parentID]
	onAcceptState := state.Diff(nil) // TODO get parent state
	// v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	blockState.timestamp = onAcceptState.GetTimestamp()
	// v.blkIDToOnAcceptState[blkID] = onAcceptState
	blockState.onAcceptState = onAcceptState

	// v.pinVerifiedBlock(b)
	v.blkIDToState[blkID] = blockState

	// TODO
	// v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

func (v *verifier2) VerifyAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()
	blockState, ok := v.blkIDToState[blkID]
	if !ok {
		blockState = &stat{}
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	// parentID := b.Parent()
	// onAcceptState := v.blkIDToOnAbortState[parentID]
	onAcceptState := state.Diff(nil)
	// v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	blockState.timestamp = onAcceptState.GetTimestamp()
	// v.blkIDToOnAcceptState[blkID] = onAcceptState
	blockState.onAcceptState = onAcceptState

	// v.pinVerifiedBlock(b)
	v.blkIDToState[blkID] = blockState

	// TODO
	// 	v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

// Assumes [b] isn't nil
func (v *verifier2) verifyCommonBlock(b stateless.CommonBlock) error {
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
