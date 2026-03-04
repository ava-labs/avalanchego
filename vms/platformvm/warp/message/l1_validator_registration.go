// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// L1ValidatorRegistration reports if a validator is registered on the P-chain.
type L1ValidatorRegistration struct {
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

// NewL1ValidatorRegistration creates a new initialized L1ValidatorRegistration.
func NewL1ValidatorRegistration(
	validationID ids.ID,
	registered bool,
) (*L1ValidatorRegistration, error) {
	msg := &L1ValidatorRegistration{
		ValidationID: validationID,
		Registered:   registered,
	}
	return msg, Initialize(msg)
}

// ParseL1ValidatorRegistration parses bytes into an initialized
// L1ValidatorRegistration.
func ParseL1ValidatorRegistration(b []byte) (*L1ValidatorRegistration, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*L1ValidatorRegistration)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
