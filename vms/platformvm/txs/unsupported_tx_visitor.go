// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
)

var (
	_ Visitor = (*unsupportedTxVisitor)(nil)

	errUnsupportedTxType = errors.New("unsupported transaction type")
)

// unsupportedTxVisitor is embedded by executors to reject, by default, tx types that
// they do not execute. Executors override the methods for the tx types they
// support.
type unsupportedTxVisitor struct{}

func (unsupportedTxVisitor) AddValidatorTx(*AddValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) AddSubnetValidatorTx(*AddSubnetValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) AddDelegatorTx(*AddDelegatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) CreateChainTx(*CreateChainTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) CreateSubnetTx(*CreateSubnetTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) ImportTx(*ImportTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) ExportTx(*ExportTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) AdvanceTimeTx(*AdvanceTimeTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) RewardValidatorTx(*RewardValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) RemoveSubnetValidatorTx(*RemoveSubnetValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) TransformSubnetTx(*TransformSubnetTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) TransferSubnetOwnershipTx(*TransferSubnetOwnershipTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) BaseTx(*BaseTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) ConvertSubnetToL1Tx(*ConvertSubnetToL1Tx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) DisableL1ValidatorTx(*DisableL1ValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) AddAutoRenewedValidatorTx(*AddAutoRenewedValidatorTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) SetAutoRenewedValidatorConfigTx(*SetAutoRenewedValidatorConfigTx) error {
	return errUnsupportedTxType
}

func (unsupportedTxVisitor) RewardAutoRenewedValidatorTx(*RewardAutoRenewedValidatorTx) error {
	return errUnsupportedTxType
}
