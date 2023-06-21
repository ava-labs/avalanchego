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

func (i *issuer) ClaimTx(*txs.ClaimTx) error {
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

func (i *issuer) BaseTx(*txs.BaseTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) MultisigAliasTx(*txs.MultisigAliasTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) AddDepositOfferTx(*txs.AddDepositOfferTx) error {
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

func (r *remover) ClaimTx(*txs.ClaimTx) error {
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

func (r *remover) BaseTx(*txs.BaseTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) MultisigAliasTx(*txs.MultisigAliasTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) AddDepositOfferTx(*txs.AddDepositOfferTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}
