// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ validatorIntf = &validatorImpl{}

type validatorIntf interface {
	Delegators() []DelegatorAndID
	SubnetValidators() map[ids.ID]SubnetValidatorAndID
}

type validatorImpl struct {
	// sorted in order of next operation, either addition or removal.
	delegators []DelegatorAndID
	// maps subnetID to tx
	subnets map[ids.ID]SubnetValidatorAndID
}

func (v *validatorImpl) Delegators() []DelegatorAndID {
	return v.delegators
}

func (v *validatorImpl) SubnetValidators() map[ids.ID]SubnetValidatorAndID {
	return v.subnets
}
