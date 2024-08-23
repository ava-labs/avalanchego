// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SetSubnetValidatorWeight is signed when the subnet wants to update the weight of a validator.
type SetSubnetValidatorWeight struct {
	ValidationID ids.ID `serialize:"true"`
	Nonce        uint64 `serialize:"true"`
	Weight       uint64 `serialize:"true"`

	bytes []byte
}

// NewSetSubnetValidatorWeight creates a new *SetSubnetValidatorWeight and initializes it.
func NewSetSubnetValidatorWeight(validationID ids.ID, nonce uint64, weight uint64) (*SetSubnetValidatorWeight, error) {
	bhp := &SetSubnetValidatorWeight{
		ValidationID: validationID,
		Nonce:        nonce,
		Weight:       weight,
	}
	return bhp, initialize(bhp)
}

// ParseSetSubnetValidatorWeight converts a slice of bytes into an initialized SetSubnetValidatorWeight.
func ParseSetSubnetValidatorWeight(b []byte) (*SetSubnetValidatorWeight, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SetSubnetValidatorWeight)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewSetSubnetValidatorWeight or Parse,
// otherwise it will return nil.
func (b *SetSubnetValidatorWeight) Bytes() []byte {
	return b.bytes
}

func (b *SetSubnetValidatorWeight) initialize(bytes []byte) {
	b.bytes = bytes
}
