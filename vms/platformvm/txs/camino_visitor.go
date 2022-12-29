// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

type CaminoVisitor interface {
	AddAddressStateTx(*AddAddressStateTx) error
	DepositTx(*DepositTx) error
	UnlockDepositTx(*UnlockDepositTx) error
	RegisterNodeTx(*RegisterNodeTx) error
}
