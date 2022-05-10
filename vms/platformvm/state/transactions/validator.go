// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ validatorCache = &validatorImpl{}

type validatorCache interface {
	Delegators() []*unsigned.AddDelegatorTx
	SubnetValidators() map[ids.ID]*unsigned.AddSubnetValidatorTx
}

type validatorImpl struct {
	// sorted in order of next operation, either addition or removal.
	delegators []*unsigned.AddDelegatorTx
	// maps subnetID to tx
	subnets map[ids.ID]*unsigned.AddSubnetValidatorTx
}

func (v *validatorImpl) Delegators() []*unsigned.AddDelegatorTx {
	return v.delegators
}

func (v *validatorImpl) SubnetValidators() map[ids.ID]*unsigned.AddSubnetValidatorTx {
	return v.subnets
}
