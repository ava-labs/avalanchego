// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
)

type StaticConfig struct {
	// Fee that is burned by every non-state creating transaction
	TxFee uint64

	// Fee that must be burned by every state creating transaction before AP3
	CreateAssetTxFee uint64

	// Fee that must be burned by every subnet creating transaction after AP3
	CreateSubnetTxFee uint64

	// Fee that must be burned by every transform subnet transaction
	TransformSubnetTxFee uint64

	// Fee that must be burned by every blockchain creating transaction after AP3
	CreateBlockchainTxFee uint64

	// Transaction fee for adding a primary network validator
	AddPrimaryNetworkValidatorFee uint64

	// Transaction fee for adding a primary network delegator
	AddPrimaryNetworkDelegatorFee uint64

	// Transaction fee for adding a subnet validator
	AddSubnetValidatorFee uint64

	// Transaction fee for adding a subnet delegator
	AddSubnetDelegatorFee uint64

	// The minimum amount of tokens one must bond to be a validator
	MinValidatorStake uint64

	// The maximum amount of tokens that can be bonded on a validator
	MaxValidatorStake uint64

	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake uint64

	// Minimum fee that can be charged for delegation
	MinDelegationFee uint32
}

func (c *StaticConfig) GetCreateBlockchainTxFee(upgrades upgrade.Config, timestamp time.Time) uint64 {
	if upgrades.IsApricotPhase3Activated(timestamp) {
		return c.CreateBlockchainTxFee
	}
	return c.CreateAssetTxFee
}

func (c *StaticConfig) GetCreateSubnetTxFee(upgrades upgrade.Config, timestamp time.Time) uint64 {
	if upgrades.IsApricotPhase3Activated(timestamp) {
		return c.CreateSubnetTxFee
	}
	return c.CreateAssetTxFee
}
