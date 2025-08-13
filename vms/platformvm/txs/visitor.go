// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	// Apricot Transactions:
	AddValidatorTx(*AddValidatorTx) error
	AddSubnetValidatorTx(*AddSubnetValidatorTx) error
	AddDelegatorTx(*AddDelegatorTx) error
	CreateChainTx(*CreateChainTx) error
	CreateSubnetTx(*CreateSubnetTx) error
	ImportTx(*ImportTx) error
	ExportTx(*ExportTx) error
	AdvanceTimeTx(*AdvanceTimeTx) error
	RewardValidatorTx(*RewardValidatorTx) error

	// Banff Transactions:
	RemoveSubnetValidatorTx(*RemoveSubnetValidatorTx) error
	TransformSubnetTx(*TransformSubnetTx) error
	AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error
	AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error

	// Durango Transactions:
	TransferSubnetOwnershipTx(*TransferSubnetOwnershipTx) error
	BaseTx(*BaseTx) error

	// Etna Transactions:
	ConvertSubnetToL1Tx(*ConvertSubnetToL1Tx) error
	RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
	SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
	IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
	DisableL1ValidatorTx(*DisableL1ValidatorTx) error
}
