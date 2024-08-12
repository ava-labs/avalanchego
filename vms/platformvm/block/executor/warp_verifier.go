// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ block.Visitor = (*warpBlockVerifier)(nil)
	_ txs.Visitor   = (*warpTxVerifier)(nil)
)

// warpBlockVerifier handles the logic for verifying all the warp message
// signatures in a block's transactions.
type warpBlockVerifier struct{}

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

func (*warpBlockVerifier) verifyStandardTxs(txs []*txs.Tx) error {
	for _, tx := range txs {
		if err := tx.Unsigned.Visit(&warpTxVerifier{}); err != nil {
			return err
		}
	}
	return nil
}

// warpBlockVerifier handles the logic for verifying the warp message
// signature in a transaction.
type warpTxVerifier struct{}

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
func (*warpTxVerifier) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*warpTxVerifier) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return nil
}
