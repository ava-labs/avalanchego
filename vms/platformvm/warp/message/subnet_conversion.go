// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SubnetConversion reports summary of the subnet conversation that occurred on
// the P-chain.
type SubnetConversion struct {
	payload

	ID ids.ID `serialize:"true" json:"id"`
}

// NewSubnetConversion creates a new initialized SubnetConversion.
func NewSubnetConversion(id ids.ID) (*SubnetConversion, error) {
	msg := &SubnetConversion{
		ID: id,
	}
	return msg, Initialize(msg)
}

// ParseSubnetConversion parses bytes into an initialized SubnetConversion.
func ParseSubnetConversion(b []byte) (*SubnetConversion, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetConversion)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
