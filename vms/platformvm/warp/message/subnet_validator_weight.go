// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SubnetValidatorWeight reports the current nonce and weight of a validator
// registered on the P-chain.
type SubnetValidatorWeight struct {
	payload

	ValidationID ids.ID `serialize:"true" json:"validationID"`
	Nonce        uint64 `serialize:"true" json:"nonce"`
	Weight       uint64 `serialize:"true" json:"weight"`
}

// NewSubnetValidatorWeight creates a new initialized SubnetValidatorWeight.
func NewSubnetValidatorWeight(
	validationID ids.ID,
	nonce uint64,
	weight uint64,
) (*SubnetValidatorWeight, error) {
	msg := &SubnetValidatorWeight{
		ValidationID: validationID,
		Nonce:        nonce,
		Weight:       weight,
	}
	return msg, Initialize(msg)
}

// ParseSubnetValidatorWeight parses bytes into an initialized
// SubnetValidatorWeight.
func ParseSubnetValidatorWeight(b []byte) (*SubnetValidatorWeight, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetValidatorWeight)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
