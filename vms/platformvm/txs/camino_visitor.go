// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

type CaminoVisitor interface {
	AddressStateTx(*AddressStateTx) error
	DepositTx(*DepositTx) error
	UnlockDepositTx(*UnlockDepositTx) error
	ClaimRewardTx(*ClaimRewardTx) error
	RegisterNodeTx(*RegisterNodeTx) error
}
