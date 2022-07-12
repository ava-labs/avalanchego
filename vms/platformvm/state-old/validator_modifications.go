// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ ValidatorModifications = &validatorModifications{}

type ValidatorModifications interface {
	Delegators() []DelegatorAndID
	SubnetValidators() map[ids.ID]SubnetValidatorAndID
}

type validatorModifications struct {
	// sorted in order of next operation, either addition or removal.
	delegators []DelegatorAndID
	// maps subnetID to tx
	subnets map[ids.ID]SubnetValidatorAndID
}

func (v *validatorModifications) Delegators() []DelegatorAndID {
	return v.delegators
}

func (v *validatorModifications) SubnetValidators() map[ids.ID]SubnetValidatorAndID {
	return v.subnets
}
