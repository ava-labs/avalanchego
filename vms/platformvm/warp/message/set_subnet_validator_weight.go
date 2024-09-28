// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SetSubnetValidatorWeight updates the weight of the specified validator.
type SetSubnetValidatorWeight struct {
	payload

	ValidationID ids.ID `serialize:"true" json:"validationID"`
	Nonce        uint64 `serialize:"true" json:"nonce"`
	Weight       uint64 `serialize:"true" json:"weight"`
}

// NewSetSubnetValidatorWeight creates a new initialized
// SetSubnetValidatorWeight.
func NewSetSubnetValidatorWeight(
	validationID ids.ID,
	nonce uint64,
	weight uint64,
) (*SetSubnetValidatorWeight, error) {
	msg := &SetSubnetValidatorWeight{
		ValidationID: validationID,
		Nonce:        nonce,
		Weight:       weight,
	}
	return msg, Initialize(msg)
}

// ParseSetSubnetValidatorWeight parses bytes into an initialized
// SetSubnetValidatorWeight.
func ParseSetSubnetValidatorWeight(b []byte) (*SetSubnetValidatorWeight, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SetSubnetValidatorWeight)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
