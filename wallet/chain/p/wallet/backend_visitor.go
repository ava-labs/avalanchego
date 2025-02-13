// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ txs.Visitor = (*backendVisitor)(nil)

	ErrUnsupportedTxType = errors.New("unsupported tx type")
)

// backendVisitor handles accepting of transactions for the backend
type backendVisitor struct {
	b    *backend
	ctx  context.Context
	txID ids.ID
}

func (*backendVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrUnsupportedTxType
}

func (*backendVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrUnsupportedTxType
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
	b.b.setOwner(
		b.txID,
		tx.Owner,
	)
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

func (b *backendVisitor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
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

func (b *backendVisitor) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	b.b.setOwner(
		tx.Subnet,
		tx.Owner,
	)
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) BaseTx(tx *txs.BaseTx) error {
	return b.baseTx(tx)
}

func (b *backendVisitor) ConvertSubnetToL1Tx(tx *txs.ConvertSubnetToL1Tx) error {
	for i, vdr := range tx.Validators {
		b.b.setOwner(
			tx.Subnet.Append(uint32(i)),
			&secp256k1fx.OutputOwners{
				Threshold: vdr.DeactivationOwner.Threshold,
				Addrs:     vdr.DeactivationOwner.Addresses,
			},
		)
	}
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	warpMessage, err := warp.ParseMessage(tx.Message)
	if err != nil {
		return err
	}
	addressedCallPayload, err := payload.ParseAddressedCall(warpMessage.Payload)
	if err != nil {
		return err
	}
	registerL1ValidatorMessage, err := message.ParseRegisterL1Validator(addressedCallPayload.Payload)
	if err != nil {
		return err
	}

	b.b.setOwner(
		registerL1ValidatorMessage.ValidationID(),
		&secp256k1fx.OutputOwners{
			Threshold: registerL1ValidatorMessage.DisableOwner.Threshold,
			Addrs:     registerL1ValidatorMessage.DisableOwner.Addresses,
		},
	)
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) IncreaseL1ValidatorBalanceTx(tx *txs.IncreaseL1ValidatorBalanceTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) DisableL1ValidatorTx(tx *txs.DisableL1ValidatorTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) baseTx(tx *txs.BaseTx) error {
	return b.b.removeUTXOs(
		b.ctx,
		constants.PlatformChainID,
		tx.InputIDs(),
	)
}
