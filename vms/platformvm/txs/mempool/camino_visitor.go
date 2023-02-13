// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Issuer

func (i *issuer) AddressStateTx(*txs.AddressStateTx) error {
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

func (i *issuer) ClaimRewardTx(*txs.ClaimRewardTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) RegisterNodeTx(*txs.RegisterNodeTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) RewardsImportTx(*txs.RewardsImportTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

// Remover

func (r *remover) AddressStateTx(*txs.AddressStateTx) error {
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

func (r *remover) ClaimRewardTx(*txs.ClaimRewardTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) RegisterNodeTx(*txs.RegisterNodeTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) RewardsImportTx(*txs.RewardsImportTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}
