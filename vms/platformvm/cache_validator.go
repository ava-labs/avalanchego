// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ validator = &validatorImpl{}

type validator interface {
	Delegators() []VerifiableUnsignedAddDelegatorTx
	SubnetValidators() map[ids.ID]VerifiableUnsignedAddSubnetValidatorTx
}

type validatorImpl struct {
	// sorted in order of next operation, either addition or removal.
	delegators []VerifiableUnsignedAddDelegatorTx
	// maps subnetID to tx
	subnets map[ids.ID]VerifiableUnsignedAddSubnetValidatorTx
}

func (v *validatorImpl) Delegators() []VerifiableUnsignedAddDelegatorTx {
	return v.delegators
}

func (v *validatorImpl) SubnetValidators() map[ids.ID]VerifiableUnsignedAddSubnetValidatorTx {
	return v.subnets
}
