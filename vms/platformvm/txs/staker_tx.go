// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"time"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/utils/crypto/bls"
	"github.com/MetalBlockchain/metalgo/vms/components/avax"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/fx"
)

// ValidatorTx defines the interface for a validator transaction that supports
// delegation.
type ValidatorTx interface {
	UnsignedTx
	PermissionlessStaker

	ValidationRewardsOwner() fx.Owner
	DelegationRewardsOwner() fx.Owner
	Shares() uint32
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
	// PublicKey returns the BLS public key registered by this transaction. If
	// there was no key registered by this transaction, it will return false.
	PublicKey() (*bls.PublicKey, bool, error)
	EndTime() time.Time
	Weight() uint64
	CurrentPriority() Priority
}

type ScheduledStaker interface {
	Staker
	StartTime() time.Time
	PendingPriority() Priority
}
