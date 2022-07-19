// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

const (
	// First subnet delegators are removed from the current validator set,
	SubnetDelegatorCurrent Priority = iota + 1
	// then subnet validators,
	SubnetValidatorCurrent
	// then primary network delegators,
	PrimaryNetworkDelegatorCurrent
	// then primary network validators.
	PrimaryNetworkValidatorCurrent
)

const (
	// First primary network delegators are moved from the pending to the
	// current validator set,
	PrimaryNetworkDelegatorPending Priority = iota + 1
	// then primary network validators,
	PrimaryNetworkValidatorPending
	// then subnet validators,
	SubnetValidatorPending
	// then subnet delegators.
	SubnetDelegatorPending
)

var PendingToCurrentPriorities = []Priority{
	PrimaryNetworkValidatorPending: PrimaryNetworkValidatorCurrent,
	PrimaryNetworkDelegatorPending: PrimaryNetworkDelegatorCurrent,
	SubnetValidatorPending:         SubnetValidatorCurrent,
	SubnetDelegatorPending:         SubnetDelegatorCurrent,
}

type Priority byte
