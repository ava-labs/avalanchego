// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ validator = &validatorImpl{}

type validator interface {
	Delegators() []txs.DelegatorAndID
	SubnetValidators() map[ids.ID]txs.SubnetValidatorAndID
}

type validatorImpl struct {
	// sorted in order of next operation, either addition or removal.
	delegators []txs.DelegatorAndID
	// maps subnetID to tx
	subnets map[ids.ID]txs.SubnetValidatorAndID
}

func (v *validatorImpl) Delegators() []txs.DelegatorAndID {
	return v.delegators
}

func (v *validatorImpl) SubnetValidators() map[ids.ID]txs.SubnetValidatorAndID {
	return v.subnets
}
