// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

const (
	// First subnet delegators are removed from the current validator set,
	SubnetDelegatorCurrentPriority = iota + 1
	// then subnet validators,
	SubnetValidatorCurrentPriority
	// then primary network delegators,
	PrimaryNetworkDelegatorCurrentPriority
	// then primary network validators.
	PrimaryNetworkValidatorCurrentPriority
)

const (
	// First primary network delegators are moved from the pending to the
	// current validator set,
	PrimaryNetworkDelegatorPendingPriority = iota + 1
	// then primary network validators,
	PrimaryNetworkValidatorPendingPriority
	// then subnet validators,
	SubnetValidatorPendingPriority
	// then subnet delegators.
	SubnetDelegatorPendingPriority
)

var PendingToCurrentPriorities = []byte{
	PrimaryNetworkValidatorPendingPriority: PrimaryNetworkValidatorCurrentPriority,
	PrimaryNetworkDelegatorPendingPriority: PrimaryNetworkDelegatorCurrentPriority,
	SubnetValidatorPendingPriority:         SubnetValidatorCurrentPriority,
	SubnetDelegatorPendingPriority:         SubnetDelegatorCurrentPriority,
}
