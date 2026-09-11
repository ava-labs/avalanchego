// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

var (
	_ platform.TxVisitor = (*UnsupportedTxVisitor)(nil)

	ErrUnsupportedTxType = errors.New("unsupported transaction type")
)

// UnsupportedTxVisitor is embedded by executors to reject, by default, tx types that
// they do not execute. Executors override the methods for the tx types they
// support.
type UnsupportedTxVisitor struct{}

func (UnsupportedTxVisitor) AddValidatorTx(*platform.AddValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) AddSubnetValidatorTx(*platform.AddSubnetValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) AddDelegatorTx(*platform.AddDelegatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) CreateChainTx(*platform.CreateChainTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) CreateSubnetTx(*platform.CreateSubnetTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) ImportTx(*platform.ImportTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) ExportTx(*platform.ExportTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) AdvanceTimeTx(*platform.AdvanceTimeTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) RewardValidatorTx(*platform.RewardValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) RemoveSubnetValidatorTx(*platform.RemoveSubnetValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) TransformSubnetTx(*platform.TransformSubnetTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) AddPermissionlessValidatorTx(*platform.AddPermissionlessValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) AddPermissionlessDelegatorTx(*platform.AddPermissionlessDelegatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) TransferSubnetOwnershipTx(*platform.TransferSubnetOwnershipTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) BaseTx(*platform.BaseTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) ConvertSubnetToL1Tx(*platform.ConvertSubnetToL1Tx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) RegisterL1ValidatorTx(*platform.RegisterL1ValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) SetL1ValidatorWeightTx(*platform.SetL1ValidatorWeightTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) IncreaseL1ValidatorBalanceTx(*platform.IncreaseL1ValidatorBalanceTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) DisableL1ValidatorTx(*platform.DisableL1ValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) AddAutoRenewedValidatorTx(*platform.AddAutoRenewedValidatorTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) SetAutoRenewedValidatorConfigTx(*platform.SetAutoRenewedValidatorConfigTx) error {
	return ErrUnsupportedTxType
}

func (UnsupportedTxVisitor) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	return ErrUnsupportedTxType
}
