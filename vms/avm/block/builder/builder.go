// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/components/fees"

	blockexecutor "github.com/ava-labs/avalanchego/vms/avm/block/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/avm/txs/executor"
)

// targetBlockSize is the max block size we aim to produce
const targetBlockSize = 128 * units.KiB

var (
	_ Builder = (*builder)(nil)

	ErrNoTransactions = errors.New("no transactions")
)

type Builder interface {
	// BuildBlock can be called to attempt to create a new block
	BuildBlock(context.Context) (snowman.Block, error)
}

// builder implements a simple builder to convert txs into valid blocks
type builder struct {
	backend *txexecutor.Backend
	manager blockexecutor.Manager
	clk     *mockable.Clock

	// Pool of all txs that may be able to be added
	mempool mempool.Mempool
}

func New(
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	clk *mockable.Clock,
	mempool mempool.Mempool,
) Builder {
	return &builder{
		backend: backend,
		manager: manager,
		clk:     clk,
		mempool: mempool,
	}
}

// BuildBlock builds a block to be added to consensus.
func (b *builder) BuildBlock(context.Context) (snowman.Block, error) {
	defer b.mempool.RequestBuildBlock()

	ctx := b.backend.Ctx
	ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferredID := b.manager.Preferred()
	preferred, err := b.manager.GetStatelessBlock(preferredID)
	if err != nil {
		return nil, err
	}

	preferredHeight := preferred.Height()
	nextHeight := preferredHeight + 1

	parentBlkTime := preferred.Timestamp()
	nextBlkTime := blockexecutor.NextBlockTime(parentBlkTime, b.clk)

	stateDiff, err := state.NewDiff(preferredID, b.manager)
	if err != nil {
		return nil, err
	}

	var (
		blockTxs      []*txs.Tx
		inputs        set.Set[ids.ID]
		remainingSize = targetBlockSize

		isEForkActive = b.backend.Config.IsEActivated(nextBlkTime)
		feesCfg       = config.GetDynamicFeesConfig(isEForkActive)
	)

	feeRates, err := stateDiff.GetFeeRates()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving fee rates: %w", err)
	}
	parentBlkComplexity, err := stateDiff.GetLastBlockComplexity()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving last block complexity: %w", err)
	}

	feeManager := fees.NewManager(feeRates)
	if isEForkActive {
		if err := feeManager.UpdateFeeRates(
			feesCfg,
			parentBlkComplexity,
			parentBlkTime.Unix(),
			nextBlkTime.Unix(),
		); err != nil {
			return nil, fmt.Errorf("failed updating fee rates, %w", err)
		}
	}

	for {
		tx, exists := b.mempool.Peek()
		if !exists {
			break
		}
		txSize := len(tx.Bytes())

		// pre e upgrade is active, we fill blocks till a target size
		// post e upgrade is active, we fill blocks till a target complexity
		done := (!isEForkActive && txSize > remainingSize) ||
			(isEForkActive && !fees.Compare(feeManager.GetCumulatedComplexity(), feesCfg.BlockTargetComplexityRate))
		if done {
			break
		}
		b.mempool.Remove(tx)

		// Invariant: [tx] has already been syntactically verified.

		txDiff, err := state.NewDiffOn(stateDiff)
		if err != nil {
			return nil, err
		}

		err = tx.Unsigned.Visit(&txexecutor.SemanticVerifier{
			Backend:            b.backend,
			BlkFeeManager:      feeManager,
			BlockMaxComplexity: feesCfg.BlockMaxComplexity,
			State:              txDiff,
			Tx:                 tx,
		})
		if err != nil {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, err)
			continue
		}

		executor := &txexecutor.Executor{
			Codec: b.backend.Codec,
			State: txDiff,
			Tx:    tx,
		}
		err = tx.Unsigned.Visit(executor)
		if err != nil {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, err)
			continue
		}

		if inputs.Overlaps(executor.Inputs) {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, blockexecutor.ErrConflictingBlockTxs)
			continue
		}
		err = b.manager.VerifyUniqueInputs(preferredID, inputs)
		if err != nil {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, err)
			continue
		}
		inputs.Union(executor.Inputs)

		txDiff.AddTx(tx)
		txDiff.SetFeeRates(feeManager.GetFeeRates())
		txDiff.SetLastBlockComplexity(feeManager.GetCumulatedComplexity())
		txDiff.Apply(stateDiff)

		if isEForkActive {
			remainingSize -= txSize
		}
		blockTxs = append(blockTxs, tx)
	}

	if len(blockTxs) == 0 {
		return nil, ErrNoTransactions
	}

	statelessBlk, err := block.NewStandardBlock(
		preferredID,
		nextHeight,
		nextBlkTime,
		blockTxs,
		b.backend.Codec,
	)
	if err != nil {
		return nil, err
	}

	return b.manager.NewBlock(statelessBlk), nil
}
