// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/ids"
)

var ErrNonceReservedForRemoval = errors.New("maxUint64 nonce is reserved for removal")

// SubnetValidatorWeight is both received and sent by the P-chain.
//
// If the P-chain is receiving this message, it is treated as a command to
// update the weight of the validator.
//
// If the P-chain is sending this message, it is reporting the current nonce and
// weight of the validator.
type SubnetValidatorWeight struct {
	payload

	ValidationID ids.ID `serialize:"true" json:"validationID"`
	Nonce        uint64 `serialize:"true" json:"nonce"`
	Weight       uint64 `serialize:"true" json:"weight"`
}

func (s *SubnetValidatorWeight) Verify() error {
	if s.Nonce == math.MaxUint64 && s.Weight != 0 {
		return ErrNonceReservedForRemoval
	}
	return nil
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
	return msg, initialize(msg)
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
