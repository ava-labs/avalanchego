// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

type PrimaryNetworkValidator struct {
	Validator *Validator

	// SubnetValidators maps SubnetID to the validator for that subnet.
	SubnetValidators map[ids.ID]*Validator
}

type Validator struct {
	Validator *Staker

	// Weight of delegations to this validator. Doesn't include this validator's
	// own weight.
	TotalDelegatorWeight uint64

	// Delegators is the list of delegators to this validator sorted by either
	// StartTime or EndTime.
	Delegators []*Staker
}
