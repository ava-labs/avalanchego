// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Issuer
func (i *issuer) AddAddressStateTx(*txs.AddAddressStateTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) DepositTx(*txs.DepositTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) UnlockDepositTx(*txs.UnlockDepositTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

// Remover
func (r *remover) AddAddressStateTx(*txs.AddAddressStateTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) DepositTx(*txs.DepositTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) UnlockDepositTx(*txs.UnlockDepositTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}
