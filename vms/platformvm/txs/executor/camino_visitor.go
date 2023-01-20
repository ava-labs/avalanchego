// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import "github.com/ava-labs/avalanchego/vms/platformvm/txs"

// Camino Visitor implementations

// Standard

func (*StandardTxExecutor) AddressStateTx(*txs.AddressStateTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) DepositTx(*txs.DepositTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) UnlockDepositTx(*txs.UnlockDepositTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) ClaimRewardTx(*txs.ClaimRewardTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return errWrongTxType
}

// Proposal

func (*ProposalTxExecutor) AddressStateTx(*txs.AddressStateTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) DepositTx(*txs.DepositTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) UnlockDepositTx(*txs.UnlockDepositTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) ClaimRewardTx(*txs.ClaimRewardTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return errWrongTxType
}

// Atomic

func (*AtomicTxExecutor) AddressStateTx(*txs.AddressStateTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) DepositTx(*txs.DepositTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) UnlockDepositTx(*txs.UnlockDepositTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) ClaimRewardTx(*txs.ClaimRewardTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return errWrongTxType
}

// MemPool

func (v *MempoolTxVerifier) AddressStateTx(tx *txs.AddressStateTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) DepositTx(tx *txs.DepositTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) UnlockDepositTx(tx *txs.UnlockDepositTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) ClaimRewardTx(tx *txs.ClaimRewardTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) RegisterNodeTx(tx *txs.RegisterNodeTx) error {
	return v.standardTx(tx)
}
