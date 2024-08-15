// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ block.Visitor = (*warpBlockVerifier)(nil)
	_ txs.Visitor   = (*warpTxVerifier)(nil)
)

// warpBlockVerifier handles the logic for verifying all the warp message
// signatures in a block's transactions.
type warpBlockVerifier struct {
	ctx          context.Context
	chainCtx     *snow.Context
	state        state.State
	pChainHeight uint64
}

// Pre-Banff did not contain any transactions with warp messages.
func (*warpBlockVerifier) ApricotAbortBlock(*block.ApricotAbortBlock) error       { return nil }
func (*warpBlockVerifier) ApricotCommitBlock(*block.ApricotCommitBlock) error     { return nil }
func (*warpBlockVerifier) ApricotProposalBlock(*block.ApricotProposalBlock) error { return nil }
func (*warpBlockVerifier) ApricotStandardBlock(*block.ApricotStandardBlock) error { return nil }
func (*warpBlockVerifier) ApricotAtomicBlock(*block.ApricotAtomicBlock) error     { return nil }

// No transactions in these blocks.
func (*warpBlockVerifier) BanffAbortBlock(*block.BanffAbortBlock) error   { return nil }
func (*warpBlockVerifier) BanffCommitBlock(*block.BanffCommitBlock) error { return nil }

func (v *warpBlockVerifier) BanffProposalBlock(b *block.BanffProposalBlock) error {
	return v.verifyStandardTxs(b.Transactions)
}

func (v *warpBlockVerifier) BanffStandardBlock(b *block.BanffStandardBlock) error {
	return v.verifyStandardTxs(b.Transactions)
}

func (v *warpBlockVerifier) verifyStandardTxs(txs []*txs.Tx) error {
	for _, tx := range txs {
		if err := tx.Unsigned.Visit(&warpTxVerifier{
			ctx:          v.ctx,
			chainCtx:     v.chainCtx,
			state:        v.state,
			pChainHeight: v.pChainHeight,
		}); err != nil {
			return err
		}
	}
	return nil
}

// warpTxVerifier handles the logic for verifying the warp message
// signature in a transaction.
type warpTxVerifier struct {
	ctx          context.Context
	chainCtx     *snow.Context
	state        state.State
	pChainHeight uint64
}

func (*warpTxVerifier) AddDelegatorTx(*txs.AddDelegatorTx) error                       { return nil }
func (*warpTxVerifier) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error           { return nil }
func (*warpTxVerifier) AddValidatorTx(*txs.AddValidatorTx) error                       { return nil }
func (*warpTxVerifier) AdvanceTimeTx(*txs.AdvanceTimeTx) error                         { return nil }
func (*warpTxVerifier) BaseTx(*txs.BaseTx) error                                       { return nil }
func (*warpTxVerifier) CreateChainTx(*txs.CreateChainTx) error                         { return nil }
func (*warpTxVerifier) CreateSubnetTx(*txs.CreateSubnetTx) error                       { return nil }
func (*warpTxVerifier) ExportTx(*txs.ExportTx) error                                   { return nil }
func (*warpTxVerifier) ImportTx(*txs.ImportTx) error                                   { return nil }
func (*warpTxVerifier) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error     { return nil }
func (*warpTxVerifier) RewardValidatorTx(*txs.RewardValidatorTx) error                 { return nil }
func (*warpTxVerifier) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error { return nil }
func (*warpTxVerifier) TransformSubnetTx(*txs.TransformSubnetTx) error                 { return nil }
func (*warpTxVerifier) ConvertSubnetTx(*txs.ConvertSubnetTx) error                     { return nil }
func (*warpTxVerifier) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*warpTxVerifier) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return nil
}

func (v *warpTxVerifier) RegisterSubnetValidatorTx(tx *txs.RegisterSubnetValidatorTx) error {
	if v.pChainHeight == 0 {
		return errors.New("pChainHeight must be > 0")
	}

	chainID, addr, err := v.state.GetSubnetManager(tx.ParsedMessage.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to lookup subnet manager for %s: %w", tx.ParsedMessage.SubnetID, err)
	}

	if tx.Message.SourceChainID != chainID {
		return fmt.Errorf("mismatched chainIDs: expected %s, got %s", chainID, tx.Message.SourceChainID)
	}

	if !bytes.Equal(tx.SourceAddress, addr) {
		return fmt.Errorf("mismatched addresses: expected %v, got %v", tx.SourceAddress, addr)
	}

	err = tx.Message.Signature.Verify(
		v.ctx,
		&tx.Message.UnsignedMessage,
		v.chainCtx.NetworkID,
		v.chainCtx.ValidatorState,
		v.pChainHeight,
		1,
		2,
	)
	if err != nil {
		return fmt.Errorf("failed to verify warp signature: %w", err)
	}

	return nil
}
