// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platform

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
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
	Weight() uint64
	CurrentPriority() Priority
}

// KeyedStaker is a staker whose transaction can register a BLS key. Only
// Primary Network validators can, so a caller that needs the key must
// type-assert to this interface rather than accepting any [Staker].
//
// Satisfying KeyedStaker does not by itself mean the transaction is on the
// Primary Network: AddPermissionlessValidatorTx serves both networks and
// carries the network in a field. SyntacticVerify is what rejects a key on a
// non-Primary-Network transaction.
type KeyedStaker interface {
	Staker
	// PublicKey returns the BLS public key registered by this transaction. If
	// there was no key registered by this transaction, it will return false.
	PublicKey() (*bls.PublicKey, bool, error)
}

type ScheduledStaker interface {
	BoundedStaker
	StartTime() time.Time
	PendingPriority() Priority
}

type BoundedStaker interface {
	Staker
	EndTime() time.Time
}
