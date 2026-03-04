// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs/executor"
)

const SyncBound = 10 * time.Second

var (
	_ snowman.Block = (*Block)(nil)

	ErrUnexpectedMerkleRoot        = errors.New("unexpected merkle root")
	ErrTimestampBeyondSyncBound    = errors.New("proposed timestamp is too far in the future relative to local time")
	ErrEmptyBlock                  = errors.New("block contains no transactions")
	ErrChildBlockEarlierThanParent = errors.New("proposed timestamp before current chain time")
	ErrConflictingBlockTxs         = errors.New("block contains conflicting transactions")
	ErrIncorrectHeight             = errors.New("block has incorrect height")
	ErrBlockNotFound               = errors.New("block not found")
)

// Exported for testing in avm package.
type Block struct {
	block.Block
	manager *manager
}

func (b *Block) Verify(context.Context) error {
	blkID := b.ID()
	if _, ok := b.manager.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	// Currently we don't populate the blocks merkle root.
	merkleRoot := b.Block.MerkleRoot()
	if merkleRoot != ids.Empty {
		return fmt.Errorf("%w: %s", ErrUnexpectedMerkleRoot, merkleRoot)
	}

	// Only allow timestamp to reasonably far forward
	newChainTime := b.Timestamp()
	now := b.manager.clk.Time()
	maxNewChainTime := now.Add(SyncBound)
	if newChainTime.After(maxNewChainTime) {
		return fmt.Errorf(
			"%w, proposed time (%s), local time (%s)",
			ErrTimestampBeyondSyncBound,
			newChainTime,
			now,
		)
	}

	txs := b.Txs()
	if len(txs) == 0 {
		return ErrEmptyBlock
	}

	// Syntactic verification is generally pretty fast, so we verify this first
	// before performing any possible DB reads.
	for _, tx := range txs {
		err := tx.Unsigned.Visit(&executor.SyntacticVerifier{
			Backend: b.manager.backend,
			Tx:      tx,
		})
		if err != nil {
			txID := tx.ID()
			b.manager.mempool.MarkDropped(txID, err)
			return fmt.Errorf("failed to syntactically verify tx %s: %w", txID, err)
		}
	}

	// Verify that the parent exists.
	parentID := b.Parent()
	parent, err := b.manager.GetStatelessBlock(parentID)
	if err != nil {
		return fmt.Errorf("failed to get parent %s: %w", parentID, err)
	}

	// Verify that currentBlkHeight = parentBlkHeight + 1.
	expectedHeight := parent.Height() + 1
	height := b.Height()
	if expectedHeight != height {
		return fmt.Errorf(
			"%w: expected height %d, got %d",
			ErrIncorrectHeight,
			expectedHeight,
			height,
		)
	}

	stateDiff, err := state.NewDiff(parentID, b.manager)
	if err != nil {
		return fmt.Errorf(
			"failed to initialize state diff on state at %s: %w",
			parentID,
			err,
		)
	}

	parentChainTime := stateDiff.GetTimestamp()
	// The proposed timestamp must not be before the parent's timestamp.
	if newChainTime.Before(parentChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s), chain time (%s)",
			ErrChildBlockEarlierThanParent,
			newChainTime,
			parentChainTime,
		)
	}

	stateDiff.SetTimestamp(newChainTime)

	blockState := &blockState{
		statelessBlock: b.Block,
		onAcceptState:  stateDiff,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	for _, tx := range txs {
		// Verify that the tx is valid according to the current state of the
		// chain.
		err := tx.Unsigned.Visit(&executor.SemanticVerifier{
			Backend: b.manager.backend,
			State:   stateDiff,
			Tx:      tx,
		})
		if err != nil {
			txID := tx.ID()
			b.manager.mempool.MarkDropped(txID, err)
			return fmt.Errorf("failed to semantically verify tx %s: %w", txID, err)
		}

		// Apply the txs state changes to the state.
		//
		// Note: This must be done inside the same loop as semantic verification
		// to ensure that semantic verification correctly accounts for
		// transactions that occurred earlier in the block.
		executor := &executor.Executor{
			Codec: b.manager.backend.Codec,
			State: stateDiff,
			Tx:    tx,
		}
		err = tx.Unsigned.Visit(executor)
		if err != nil {
			txID := tx.ID()
			b.manager.mempool.MarkDropped(txID, err)
			return fmt.Errorf("failed to execute tx %s: %w", txID, err)
		}

		// Verify that the transaction we just executed didn't consume inputs
		// that were already imported in a previous transaction.
		if blockState.importedInputs.Overlaps(executor.Inputs) {
			txID := tx.ID()
			b.manager.mempool.MarkDropped(txID, ErrConflictingBlockTxs)
			return ErrConflictingBlockTxs
		}
		blockState.importedInputs.Union(executor.Inputs)

		// Now that the tx would be marked as accepted, we should add it to the
		// state for the next transaction in the block.
		stateDiff.AddTx(tx)

		for chainID, txRequests := range executor.AtomicRequests {
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

	// Verify that none of the transactions consumed any inputs that were
	// already imported in a currently processing block.
	err = b.manager.VerifyUniqueInputs(parentID, blockState.importedInputs)
	if err != nil {
		return fmt.Errorf(
			"failed to verify unique inputs on state at %s: %w",
			parent,
			err,
		)
	}

	// Now that the block has been executed, we can add the block data to the
	// state diff.
	stateDiff.SetLastAccepted(blkID)
	stateDiff.AddBlock(b.Block)

	b.manager.blkIDToState[blkID] = blockState
	b.manager.mempool.Remove(txs...)
	return nil
}

func (b *Block) Accept(context.Context) error {
	blkID := b.ID()
	defer b.manager.free(blkID)

	txs := b.Txs()
	for _, tx := range txs {
		b.manager.onAccept(tx)
	}

	b.manager.lastAccepted = blkID
	b.manager.mempool.Remove(txs...)

	blkState, ok := b.manager.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrBlockNotFound, blkID)
	}

	// Update the state to reflect the changes made in [onAcceptState].
	blkState.onAcceptState.Apply(b.manager.state)

	defer b.manager.state.Abort()
	batch, err := b.manager.state.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to stage state diff for block %s: %w",
			blkID,
			err,
		)
	}

	// Note that this method writes [batch] to the database.
	if err := b.manager.backend.Ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply state diff to shared memory: %w", err)
	}

	if err := b.manager.metrics.MarkBlockAccepted(b); err != nil {
		return err
	}

	b.manager.backend.Ctx.Log.Trace(
		"accepted block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
		zap.Stringer("checksum", b.manager.state.Checksum()),
	)
	return nil
}

func (b *Block) Reject(context.Context) error {
	blkID := b.ID()
	defer b.manager.free(blkID)

	b.manager.backend.Ctx.Log.Verbo(
		"rejecting block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
	)

	for _, tx := range b.Txs() {
		if err := b.manager.VerifyTx(tx); err != nil {
			b.manager.backend.Ctx.Log.Debug("dropping invalidated tx",
				zap.Stringer("txID", tx.ID()),
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
			continue
		}
		if err := b.manager.mempool.Add(tx); err != nil {
			b.manager.backend.Ctx.Log.Debug("dropping valid tx",
				zap.Stringer("txID", tx.ID()),
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
		}
	}
	return nil
}
