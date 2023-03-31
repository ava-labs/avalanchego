// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import "github.com/ava-labs/avalanchego/vms/platformvm/txs"

var _ txs.Visitor = (*remover)(nil)

type remover struct {
	m  *mempool
	tx *txs.Tx
}

func (r *remover) AddValidatorTx(*txs.AddValidatorTx) error {
	r.m.removeStakerTx(r.tx)
	return nil
}

func (r *remover) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	r.m.removeStakerTx(r.tx)
	return nil
}

func (r *remover) AddDelegatorTx(*txs.AddDelegatorTx) error {
	r.m.removeStakerTx(r.tx)
	return nil
}

func (r *remover) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) CreateChainTx(*txs.CreateChainTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) CreateSubnetTx(*txs.CreateSubnetTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) ImportTx(*txs.ImportTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) ExportTx(*txs.ExportTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) TransformSubnetTx(*txs.TransformSubnetTx) error {
	r.m.removeDecisionTxs([]*txs.Tx{r.tx})
	return nil
}

func (r *remover) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	r.m.removeStakerTx(r.tx)
	return nil
}

func (r *remover) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	r.m.removeStakerTx(r.tx)
	return nil
}

func (*remover) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	// this tx is never in mempool
	return nil
}

func (*remover) RewardValidatorTx(*txs.RewardValidatorTx) error {
	// this tx is never in mempool
	return nil
}
