// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ txs.Visitor = (*visitor)(nil)

	ErrUnsupportedTxType = errors.New("unsupported tx type")
)

// visitor handles accepting transactions for the backend
type visitor struct {
	b    *backend
	ctx  context.Context
	txID ids.ID
}

func (*visitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrUnsupportedTxType
}

func (*visitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrUnsupportedTxType
}

func (b *visitor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) CreateChainTx(tx *txs.CreateChainTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	b.b.setSubnetOwner(
		b.txID,
		tx.Owner,
	)
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	b.b.setSubnetOwner(
		tx.Subnet,
		tx.Owner,
	)
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) BaseTx(tx *txs.BaseTx) error {
	return b.baseTx(tx)
}

func (b *visitor) ImportTx(tx *txs.ImportTx) error {
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

func (b *visitor) ExportTx(tx *txs.ExportTx) error {
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

func (b *visitor) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *visitor) baseTx(tx *txs.BaseTx) error {
	return b.b.removeUTXOs(
		b.ctx,
		constants.PlatformChainID,
		tx.InputIDs(),
	)
}
