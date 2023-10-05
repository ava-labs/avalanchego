// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

type CaminoVisitor interface {
	AddressStateTx(*AddressStateTx) error
	DepositTx(*DepositTx) error
	UnlockDepositTx(*UnlockDepositTx) error
	ClaimTx(*ClaimTx) error
	RegisterNodeTx(*RegisterNodeTx) error
	RewardsImportTx(*RewardsImportTx) error
	BaseTx(*BaseTx) error
	MultisigAliasTx(*MultisigAliasTx) error
	AddDepositOfferTx(*AddDepositOfferTx) error
	AddProposalTx(*AddProposalTx) error
	AddVoteTx(*AddVoteTx) error
	FinishProposalsTx(*FinishProposalsTx) error
}
