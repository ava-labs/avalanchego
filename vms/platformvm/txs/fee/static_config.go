// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

type StaticConfig struct {
	// Fee that is burned by every non-state creating transaction
	TxFee uint64 `json:"txFee"`

	// Fee that must be burned by every subnet creating transaction
	CreateSubnetTxFee uint64 `json:"createSubnetTxFee"`

	// Fee that must be burned by every transform subnet transaction
	TransformSubnetTxFee uint64 `json:"transformSubnetTxFee"`

	// Fee that must be burned by every blockchain creating transaction
	CreateBlockchainTxFee uint64 `json:"createBlockchainTxFee"`

	// Transaction fee for adding a primary network validator
	AddPrimaryNetworkValidatorFee uint64 `json:"addPrimaryNetworkValidatorFee"`

	// Transaction fee for adding a primary network delegator
	AddPrimaryNetworkDelegatorFee uint64 `json:"addPrimaryNetworkDelegatorFee"`

	// Transaction fee for adding a subnet validator
	AddSubnetValidatorFee uint64 `json:"addSubnetValidatorFee"`

	// Transaction fee for adding a subnet delegator
	AddSubnetDelegatorFee uint64 `json:"addSubnetDelegatorFee"`
}
