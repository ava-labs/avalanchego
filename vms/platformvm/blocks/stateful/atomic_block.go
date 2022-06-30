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
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	ErrConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")

	_ Block    = &AtomicBlock{}
	_ Decision = &AtomicBlock{}
)

// AtomicBlock being accepted results in the atomic transaction contained in the
// block to be accepted and committed to the chain.
type AtomicBlock struct {
	*stateless.AtomicBlock
	*decisionBlock

	// inputs are the atomic inputs that are consumed by this block's atomic
	// transaction
	inputs ids.Set

	atomicRequests map[ids.ID]*atomic.Requests
}

// NewAtomicBlock returns a new *AtomicBlock where the block's parent, a
// decision block, has ID [parentID].
func NewAtomicBlock(
	verifier Verifier,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*AtomicBlock, error) {
	statelessBlk, err := stateless.NewAtomicBlock(parentID, height, tx)
	if err != nil {
		return nil, err
	}
	return toStatefulAtomicBlock(statelessBlk, verifier, txExecutorBackend, choices.Processing)
}

func toStatefulAtomicBlock(
	statelessBlk *stateless.AtomicBlock,
	verifier Verifier,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (*AtomicBlock, error) {
	ab := &AtomicBlock{
		AtomicBlock: statelessBlk,
		decisionBlock: &decisionBlock{
			commonBlock: &commonBlock{
				baseBlk:           &statelessBlk.CommonBlock,
				status:            status,
				verifier:          verifier,
				txExecutorBackend: txExecutorBackend,
			},
		},
	}

	ab.Tx.Unsigned.InitCtx(ab.txExecutorBackend.Ctx)
	return ab, nil
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
	if err := ab.verify(); err != nil {
		return err
	}

	parentIntf, err := ab.parentBlock()
	if err != nil {
		return err
	}

	// AtomicBlock is not a modifier on a proposal block, so its parent must be
	// a decision.
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}

	parentState := parent.OnAccept()

	cfg := ab.txExecutorBackend.Cfg
	currentTimestamp := parentState.GetTimestamp()
	enabledAP5 := !currentTimestamp.Before(cfg.ApricotPhase5Time)

	if enabledAP5 {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			cfg.ApricotPhase5Time.Unix(),
		)
	}

	atomicExecutor := executor.AtomicTxExecutor{
		Backend:     &ab.txExecutorBackend,
		ParentState: parentState,
		Tx:          ab.Tx,
	}
	err = ab.Tx.Unsigned.Visit(&atomicExecutor)
	if err != nil {
		txID := ab.Tx.ID()
		ab.verifier.MarkDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(ab.Tx, status.Committed)

	ab.onAcceptState = atomicExecutor.OnAccept
	ab.inputs = atomicExecutor.Inputs
	ab.atomicRequests = atomicExecutor.AtomicRequests
	ab.timestamp = atomicExecutor.OnAccept.GetTimestamp()

	conflicts, err := parentIntf.conflicts(ab.inputs)
	if err != nil {
		return err
	}
	if conflicts {
		return ErrConflictingParentTxs
	}

	ab.verifier.RemoveDecisionTxs([]*txs.Tx{ab.Tx})
	ab.verifier.CacheVerifiedBlock(ab)
	parentIntf.addChild(ab)
	return nil
}

func (ab *AtomicBlock) Accept() error {
	blkID := ab.ID()

	ab.txExecutorBackend.Ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		ab.Height(),
		ab.Parent(),
	)

	ab.accept()
	ab.verifier.AddStatelessBlock(ab.AtomicBlock, ab.Status())
	if err := ab.verifier.MarkAccepted(ab.AtomicBlock); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	ab.onAcceptState.Apply(ab.verifier.GetState())

	defer ab.verifier.Abort()
	batch, err := ab.verifier.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err = ab.txExecutorBackend.Ctx.SharedMemory.Apply(ab.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			ab.AtomicBlock.Tx.ID(),
			blkID,
			err,
		)
	}

	for _, child := range ab.children {
		child.setBaseState()
	}
	if ab.onAcceptFunc != nil {
		ab.onAcceptFunc()
	}

	ab.free()
	return nil
}

func (ab *AtomicBlock) Reject() error {
	ab.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Atomic Block %s at height %d with parent %s",
		ab.ID(),
		ab.Height(),
		ab.Parent(),
	)

	if err := ab.verifier.Add(ab.Tx); err != nil {
		ab.txExecutorBackend.Ctx.Log.Debug(
			"failed to reissue tx %q due to: %s",
			ab.Tx.ID(),
			err,
		)
	}

	defer ab.reject()
	ab.verifier.AddStatelessBlock(ab.AtomicBlock, ab.Status())
	return ab.verifier.Commit()
}
