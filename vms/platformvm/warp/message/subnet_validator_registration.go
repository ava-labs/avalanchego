// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SubnetValidatorRegistration reports if a validator is registered on the
// P-chain.
type SubnetValidatorRegistration struct {
	payload

	ValidationID ids.ID `serialize:"true" json:"validationID"`
	// Registered being true means that validationID is currently a validator on
	// the P-chain.
	//
	// Registered being false means that validationID is not and can never
	// become a validator on the P-chain. It is possible that validationID was
	// previously a validator on the P-chain.
	Registered bool `serialize:"true" json:"registered"`
}

// NewSubnetValidatorRegistration creates a new initialized
// SubnetValidatorRegistration.
func NewSubnetValidatorRegistration(
	validationID ids.ID,
	registered bool,
) (*SubnetValidatorRegistration, error) {
	msg := &SubnetValidatorRegistration{
		ValidationID: validationID,
		Registered:   registered,
	}
	return msg, initialize(msg)
}

// ParseSubnetValidatorRegistration parses bytes into an initialized
// SubnetValidatorRegistration.
func ParseSubnetValidatorRegistration(b []byte) (*SubnetValidatorRegistration, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetValidatorRegistration)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
