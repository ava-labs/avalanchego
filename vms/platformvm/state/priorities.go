// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

const (
	// First subnet delegators are removed from the current validator set,
	SubnetDelegatorCurrentPriority = iota
	// then subnet validators,
	SubnetValidatorCurrentPriority
	// then primary network delegators,
	PrimaryNetworkDelegatorCurrentPriority
	// then primary network validators.
	PrimaryNetworkValidatorCurrentPriority
)

const (
	// First primary network validators are moved from the pending to the
	// current validator set,
	PrimaryNetworkValidatorPendingPriority = iota
	// then primary network delegators,
	PrimaryNetworkDelegatorPendingPriority
	// then subnet validators,
	SubnetValidatorPendingPriority
	// then subnet delegators.
	SubnetDelegatorPendingPriority
)

var PendingToCurrentPriorities = []int{
	PrimaryNetworkValidatorPendingPriority: PrimaryNetworkValidatorCurrentPriority,
	PrimaryNetworkDelegatorPendingPriority: PrimaryNetworkDelegatorCurrentPriority,
	SubnetValidatorPendingPriority:         SubnetValidatorCurrentPriority,
	SubnetDelegatorPendingPriority:         SubnetDelegatorCurrentPriority,
}
