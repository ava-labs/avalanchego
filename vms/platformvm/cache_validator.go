// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/chain4travel/caminogo/ids"
)

var _ validator = &validatorImpl{}

type validator interface {
	Delegators() []*UnsignedAddDelegatorTx
	SubnetValidators() map[ids.ID]*UnsignedAddSubnetValidatorTx
}

type validatorImpl struct {
	// sorted in order of next operation, either addition or removal.
	delegators []*UnsignedAddDelegatorTx
	// maps subnetID to tx
	subnets map[ids.ID]*UnsignedAddSubnetValidatorTx
}

func (v *validatorImpl) Delegators() []*UnsignedAddDelegatorTx {
	return v.delegators
}

func (v *validatorImpl) SubnetValidators() map[ids.ID]*UnsignedAddSubnetValidatorTx {
	return v.subnets
}
