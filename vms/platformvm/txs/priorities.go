// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

const (
	// First primary network apricot delegators are moved from the pending to
	// the current validator set,
	PrimaryNetworkDelegatorApricotPendingPriority Priority = iota + 1
	// then primary network validators,
	PrimaryNetworkValidatorPendingPriority
	// then primary network banff delegators,
	PrimaryNetworkDelegatorBanffPendingPriority
	// then permissionless subnet validators,
	SubnetPermissionlessValidatorPendingPriority
	// then permissionless subnet delegators.
	SubnetPermissionlessDelegatorPendingPriority
	// then permissioned subnet validators,
	SubnetPermissionedValidatorPendingPriority

	// First permissioned subnet validators are removed from the current
	// validator set,
	// Invariant: All permissioned stakers must be removed first because they
	//            are removed by the advancement of time. Permissionless stakers
	//            are removed with a RewardValidatorTx after time has advanced.
	SubnetPermissionedValidatorCurrentPriority
	// then permissionless subnet delegators,
	SubnetPermissionlessDelegatorCurrentPriority
	// then permissionless subnet validators,
	SubnetPermissionlessValidatorCurrentPriority
	// then primary network delegators,
	PrimaryNetworkDelegatorCurrentPriority
	// then primary network validators.
	PrimaryNetworkValidatorCurrentPriority
)

var PendingToCurrentPriorities = []Priority{
	PrimaryNetworkDelegatorApricotPendingPriority: PrimaryNetworkDelegatorCurrentPriority,
	PrimaryNetworkValidatorPendingPriority:        PrimaryNetworkValidatorCurrentPriority,
	PrimaryNetworkDelegatorBanffPendingPriority:   PrimaryNetworkDelegatorCurrentPriority,
	SubnetPermissionlessValidatorPendingPriority:  SubnetPermissionlessValidatorCurrentPriority,
	SubnetPermissionlessDelegatorPendingPriority:  SubnetPermissionlessDelegatorCurrentPriority,
	SubnetPermissionedValidatorPendingPriority:    SubnetPermissionedValidatorCurrentPriority,
}

type Priority byte
