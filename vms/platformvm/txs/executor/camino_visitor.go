// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
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

func (*StandardTxExecutor) ClaimTx(*txs.ClaimTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) RewardsImportTx(*txs.RewardsImportTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) BaseTx(*txs.BaseTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) MultisigAliasTx(*txs.MultisigAliasTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) AddDepositOfferTx(*txs.AddDepositOfferTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) AddProposalTx(*txs.AddProposalTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) AddVoteTx(*txs.AddVoteTx) error {
	return errWrongTxType
}

func (*StandardTxExecutor) FinishProposalsTx(*txs.FinishProposalsTx) error {
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

func (*ProposalTxExecutor) ClaimTx(*txs.ClaimTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) RewardsImportTx(*txs.RewardsImportTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) BaseTx(*txs.BaseTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) MultisigAliasTx(*txs.MultisigAliasTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) AddDepositOfferTx(*txs.AddDepositOfferTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) AddProposalTx(*txs.AddProposalTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) AddVoteTx(*txs.AddVoteTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) FinishProposalsTx(*txs.FinishProposalsTx) error {
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

func (*AtomicTxExecutor) ClaimTx(*txs.ClaimTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) RewardsImportTx(*txs.RewardsImportTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) BaseTx(*txs.BaseTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) MultisigAliasTx(*txs.MultisigAliasTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AddDepositOfferTx(*txs.AddDepositOfferTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AddProposalTx(*txs.AddProposalTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AddVoteTx(*txs.AddVoteTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) FinishProposalsTx(*txs.FinishProposalsTx) error {
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

func (v *MempoolTxVerifier) ClaimTx(tx *txs.ClaimTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) RegisterNodeTx(tx *txs.RegisterNodeTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) RewardsImportTx(tx *txs.RewardsImportTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) BaseTx(tx *txs.BaseTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) MultisigAliasTx(tx *txs.MultisigAliasTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) AddDepositOfferTx(tx *txs.AddDepositOfferTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) AddProposalTx(tx *txs.AddProposalTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) AddVoteTx(tx *txs.AddVoteTx) error {
	return v.standardTx(tx)
}

func (*MempoolTxVerifier) FinishProposalsTx(*txs.FinishProposalsTx) error {
	return errWrongTxType
}
