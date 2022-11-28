// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = (*backendVisitor)(nil)

// backendVisitor handles accepting of transactions for the backend
type backendVisitor struct {
	b    *backend
	ctx  stdcontext.Context
	txID ids.ID
}

func (*backendVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return errUnsupportedTxType
}

func (*backendVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return errUnsupportedTxType
}

func (b *backendVisitor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) CreateChainTx(tx *txs.CreateChainTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) ImportTx(tx *txs.ImportTx) error {
	err := b.b.removeUTXOs(
		b.ctx,
		tx.SourceChain,
		tx.InputUTXOs(),
	)
	if err != nil {
		return err
	}
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) ExportTx(tx *txs.ExportTx) error {
	for i, out := range tx.ExportedOutputs {
		err := b.b.AddUTXO(
			b.ctx,
			tx.DestinationChain,
			&avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        b.txID,
					OutputIndex: uint32(len(tx.Outs) + i),
				},
				Asset: avax.Asset{ID: out.AssetID()},
				Out:   out.Out,
			},
		)
		if err != nil {
			return err
		}
	}
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) baseTx(tx *txs.BaseTx) error {
	return b.b.removeUTXOs(
		b.ctx,
		constants.PlatformChainID,
		tx.InputIDs(),
	)
}
