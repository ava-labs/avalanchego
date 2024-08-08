// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SubnetValidatorRegistration is signed when the subnet wants to update the weight of a validator.
type SubnetValidatorRegistration struct {
	ValidationID ids.ID `serialize:"true"`
	Registered   bool   `serialize:"true"`

	bytes []byte
}

// NewSubnetValidatorRegistration creates a new *SubnetValidatorRegistration and initializes it.
func NewSubnetValidatorRegistration(validationID ids.ID, registered bool) (*SubnetValidatorRegistration, error) {
	bhp := &SubnetValidatorRegistration{
		ValidationID: validationID,
		Registered:   registered,
	}
	return bhp, initialize(bhp)
}

// ParseSubnetValidatorRegistration converts a slice of bytes into an initialized SubnetValidatorRegistration.
func ParseSubnetValidatorRegistration(b []byte) (*SubnetValidatorRegistration, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetValidatorRegistration)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewSubnetValidatorRegistration or Parse.
func (b *SubnetValidatorRegistration) Bytes() []byte {
	return b.bytes
}

func (b *SubnetValidatorRegistration) initialize(bytes []byte) {
	b.bytes = bytes
}
