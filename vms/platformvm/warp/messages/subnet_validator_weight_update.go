// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SubnetValidatorWeightUpdate is signed when the subnet wants to update the weight of a validator.
type SubnetValidatorWeightUpdate struct {
	ValidationID ids.ID `serialize:"true"`
	Nonce        uint64 `serialize:"true"`
	Weight       uint64 `serialize:"true"`

	bytes []byte
}

// NewSubnetValidatorWeightUpdate creates a new *SubnetValidatorWeightUpdate and initializes it.
func NewSubnetValidatorWeightUpdate(validationID ids.ID, nonce uint64, weight uint64) (*SubnetValidatorWeightUpdate, error) {
	bhp := &SubnetValidatorWeightUpdate{
		ValidationID: validationID,
		Nonce:        nonce,
		Weight:       weight,
	}
	return bhp, initialize(bhp)
}

// ParseSubnetValidatorWeightUpdate converts a slice of bytes into an initialized SubnetValidatorWeightUpdate.
func ParseSubnetValidatorWeightUpdate(b []byte) (*SubnetValidatorWeightUpdate, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetValidatorWeightUpdate)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewSubnetValidatorWeightUpdate or Parse.
func (b *SubnetValidatorWeightUpdate) Bytes() []byte {
	return b.bytes
}

func (b *SubnetValidatorWeightUpdate) initialize(bytes []byte) {
	b.bytes = bytes
}
