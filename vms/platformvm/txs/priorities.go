// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

const (
	// First primary network delegators are moved from the pending to the
	// current validator set,
	PrimaryNetworkDelegatorPendingPriority Priority = iota + 1
	// then primary network validators,
	PrimaryNetworkValidatorPendingPriority
	// then permissioned subnet validators,
	SubnetPermissionedValidatorPendingPriority
	// then permissionless subnet validators,
	SubnetPermissionlessValidatorPendingPriority
	// then permissionless subnet delegators.
	SubnetPermissionlessDelegatorPendingPriority

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
	PrimaryNetworkValidatorPendingPriority:       PrimaryNetworkValidatorCurrentPriority,
	PrimaryNetworkDelegatorPendingPriority:       PrimaryNetworkDelegatorCurrentPriority,
	SubnetPermissionedValidatorPendingPriority:   SubnetPermissionedValidatorCurrentPriority,
	SubnetPermissionlessValidatorPendingPriority: SubnetPermissionlessValidatorCurrentPriority,
	SubnetPermissionlessDelegatorPendingPriority: SubnetPermissionlessDelegatorCurrentPriority,
}

type Priority byte
