// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ txs.Visitor = (*wrongTxType)(nil)

	errWrongTxType = errors.New("wrong transaction type")
)

// wrongTxType is embedded by executors to reject, by default, tx types that
// they do not execute. Executors override the methods for the tx types they
// support.
type wrongTxType struct{}

func (wrongTxType) AddValidatorTx(*txs.AddValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return errWrongTxType
}

func (wrongTxType) CreateChainTx(*txs.CreateChainTx) error {
	return errWrongTxType
}

func (wrongTxType) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return errWrongTxType
}

func (wrongTxType) ImportTx(*txs.ImportTx) error {
	return errWrongTxType
}

func (wrongTxType) ExportTx(*txs.ExportTx) error {
	return errWrongTxType
}

func (wrongTxType) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return errWrongTxType
}

func (wrongTxType) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return errWrongTxType
}

func (wrongTxType) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return errWrongTxType
}

func (wrongTxType) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return errWrongTxType
}

func (wrongTxType) BaseTx(*txs.BaseTx) error {
	return errWrongTxType
}

func (wrongTxType) ConvertSubnetToL1Tx(*txs.ConvertSubnetToL1Tx) error {
	return errWrongTxType
}

func (wrongTxType) RegisterL1ValidatorTx(*txs.RegisterL1ValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) SetL1ValidatorWeightTx(*txs.SetL1ValidatorWeightTx) error {
	return errWrongTxType
}

func (wrongTxType) IncreaseL1ValidatorBalanceTx(*txs.IncreaseL1ValidatorBalanceTx) error {
	return errWrongTxType
}

func (wrongTxType) DisableL1ValidatorTx(*txs.DisableL1ValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) AddAutoRenewedValidatorTx(*txs.AddAutoRenewedValidatorTx) error {
	return errWrongTxType
}

func (wrongTxType) SetAutoRenewedValidatorConfigTx(*txs.SetAutoRenewedValidatorConfigTx) error {
	return errWrongTxType
}

func (wrongTxType) RewardAutoRenewedValidatorTx(*txs.RewardAutoRenewedValidatorTx) error {
	return errWrongTxType
}
