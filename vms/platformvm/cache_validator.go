// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
)

var _ validator = &validatorImpl{}

type validator interface {
	Delegators() []signed.DelegatorAndID
	SubnetValidators() map[ids.ID]signed.SubnetValidatorAndID
}

type validatorImpl struct {
	// sorted in order of next operation, either addition or removal.
	delegators []signed.DelegatorAndID
	// maps subnetID to tx
	subnets map[ids.ID]signed.SubnetValidatorAndID
}

func (v *validatorImpl) Delegators() []signed.DelegatorAndID {
	return v.delegators
}

func (v *validatorImpl) SubnetValidators() map[ids.ID]signed.SubnetValidatorAndID {
	return v.subnets
}
