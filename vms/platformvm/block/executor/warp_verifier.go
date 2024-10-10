// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ block.Visitor = (*warpVerifier)(nil)

// VerifyWarpMessages verifies all warp messages in the block. If any of the
// warp messages are invalid, an error is returned.
func VerifyWarpMessages(
	ctx context.Context,
	networkID uint32,
	validatorState validators.State,
	pChainHeight uint64,
	b block.Block,
) error {
	return b.Visit(&warpVerifier{
		ctx:            ctx,
		networkID:      networkID,
		validatorState: validatorState,
		pChainHeight:   pChainHeight,
	})
}

type warpVerifier struct {
	ctx            context.Context
	networkID      uint32
	validatorState validators.State
	pChainHeight   uint64
}

func (*warpVerifier) BanffAbortBlock(*block.BanffAbortBlock) error {
	return nil
}

func (*warpVerifier) BanffCommitBlock(*block.BanffCommitBlock) error {
	return nil
}

func (w *warpVerifier) BanffProposalBlock(blk *block.BanffProposalBlock) error {
	for _, tx := range blk.Transactions {
		if err := w.verify(tx); err != nil {
			return err
		}
	}
	return w.ApricotProposalBlock(&blk.ApricotProposalBlock)
}

func (w *warpVerifier) BanffStandardBlock(blk *block.BanffStandardBlock) error {
	return w.ApricotStandardBlock(&blk.ApricotStandardBlock)
}

func (*warpVerifier) ApricotAbortBlock(*block.ApricotAbortBlock) error {
	return nil
}

func (*warpVerifier) ApricotCommitBlock(*block.ApricotCommitBlock) error {
	return nil
}

func (w *warpVerifier) ApricotProposalBlock(blk *block.ApricotProposalBlock) error {
	return w.verify(blk.Tx)
}

func (w *warpVerifier) ApricotStandardBlock(blk *block.ApricotStandardBlock) error {
	for _, tx := range blk.Transactions {
		if err := w.verify(tx); err != nil {
			return err
		}
	}
	return nil
}

func (w *warpVerifier) ApricotAtomicBlock(blk *block.ApricotAtomicBlock) error {
	return w.verify(blk.Tx)
}

func (w *warpVerifier) verify(tx *txs.Tx) error {
	return executor.VerifyWarpMessages(
		w.ctx,
		w.networkID,
		w.validatorState,
		w.pChainHeight,
		tx.Unsigned,
	)
}
