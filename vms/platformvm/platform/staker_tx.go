// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platform

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

// PermissionlessValidatorTx is the sealed set of transactions that register a
// permissionless validator, which supports delegation.
type PermissionlessValidatorTx interface {
	UnsignedTx
	PermissionlessStaker
	ValidatorTx

	// PublicKey returns the BLS public key registered by this transaction. If
	// there was no key registered by this transaction, it will return false.
	PublicKey() (*bls.PublicKey, bool, error)
	ValidationRewardsOwner() fx.Owner
	DelegationRewardsOwner() fx.Owner
	Shares() uint32
}

// ValidatorTx is the sealed set of staker transactions that register a
// validator.
// TODO rename to Validator
type ValidatorTx interface {
	Staker
	validatorStaker()
}

type DelegatorTx interface {
	UnsignedTx
	PermissionlessStaker

	RewardsOwner() fx.Owner
}

type StakerTx interface {
	UnsignedTx
	Staker
}

type PermissionlessStaker interface {
	Staker

	Outputs() []*avax.TransferableOutput
	Stake() []*avax.TransferableOutput
}

type Staker interface {
	SubnetID() ids.ID
	NodeID() ids.NodeID
	Weight() uint64
	CurrentPriority() Priority
}

type ScheduledStaker interface {
	BoundedStaker
	StartTime() time.Time
	PendingPriority() Priority
}

// Delegator is the sealed set of staker transactions that register a
// delegator.
type Delegator interface {
	ScheduledStaker
	delegator()
}

type BoundedStaker interface {
	Staker
	EndTime() time.Time
}
