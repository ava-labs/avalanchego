// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var _ block.Visitor = (*rejector)(nil)

// rejector handles the logic for rejecting a block.
// All errors returned by this struct are fatal and should result in the chain
// being shutdown.
type rejector struct {
	*backend

	txExecutorBackend *executor.Backend
	addTxsToMempool   bool
}

func (r *rejector) BanffAbortBlock(b *block.BanffAbortBlock) error {
	return r.rejectBlock(b, "banff abort")
}

func (r *rejector) BanffCommitBlock(b *block.BanffCommitBlock) error {
	return r.rejectBlock(b, "banff commit")
}

func (r *rejector) BanffProposalBlock(b *block.BanffProposalBlock) error {
	return r.rejectBlock(b, "banff proposal")
}

func (r *rejector) BanffStandardBlock(b *block.BanffStandardBlock) error {
	return r.rejectBlock(b, "banff standard")
}

func (r *rejector) ApricotAbortBlock(b *block.ApricotAbortBlock) error {
	return r.rejectBlock(b, "apricot abort")
}

func (r *rejector) ApricotCommitBlock(b *block.ApricotCommitBlock) error {
	return r.rejectBlock(b, "apricot commit")
}

func (r *rejector) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	return r.rejectBlock(b, "apricot proposal")
}

func (r *rejector) ApricotStandardBlock(b *block.ApricotStandardBlock) error {
	return r.rejectBlock(b, "apricot standard")
}

func (r *rejector) ApricotAtomicBlock(b *block.ApricotAtomicBlock) error {
	return r.rejectBlock(b, "apricot atomic")
}

func (r *rejector) rejectBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
	)

	if !r.addTxsToMempool {
		return nil
	}

	preferredState, err := state.NewDiff(r.backend.Preferred(), r.backend)
	if err != nil {
		return err
	}

	var (
		currentTimestamp = preferredState.GetTimestamp()
		cfg              = r.txExecutorBackend.Config
		isEActive        = cfg.IsEActivated(currentTimestamp)
		feesCfg          = config.GetDynamicFeesConfig(isEActive)
	)

	feeRates, err := preferredState.GetFeeRates()
	if err != nil {
		return err
	}
	feeManager := commonfees.NewManager(feeRates)
	if isEActive {
		nextBlkTime, _, err := state.NextBlockTime(preferredState, r.txExecutorBackend.Clk)
		if err != nil {
			return err
		}

		feeManager, err = fees.UpdatedFeeManager(preferredState, cfg, currentTimestamp, nextBlkTime)
		if err != nil {
			return err
		}
	}

	for _, tx := range b.Txs() {
		// With dynamic fees, the rejected txs may not be able to pay the updated required fees.
		// We recheck only the fees, withouth re-validating the whole transaction.
		feeManager.ResetComplexity()

		feeCalculator := fees.Calculator{
			IsEActive:          isEActive,
			Config:             cfg,
			ChainTime:          currentTimestamp,
			FeeManager:         feeManager,
			BlockMaxComplexity: feesCfg.BlockMaxComplexity,
			Credentials:        tx.Creds,
		}
		if err := tx.Unsigned.Visit(&feeCalculator); err != nil {
			r.ctx.Log.Info(
				"tx failed fees checks",
				zap.Stringer("txID", tx.ID()),
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
		}

		// TODO ABENEGIA: re-validate txs here. Tip may have changed due to change in base fee!!!
		if err := r.Mempool.Add(tx, feeCalculator.TipPercentage); err != nil {
			r.ctx.Log.Debug(
				"failed to reissue tx",
				zap.Stringer("txID", tx.ID()),
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
		}
	}

	r.Mempool.RequestBuildBlock(false /*=emptyBlockPermitted*/)

	return nil
}
