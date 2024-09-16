// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SubnetConversion is signed when
type SubnetConversion struct {
	SubnetConversionID ids.ID `serialize:"true"`

	bytes []byte
}

// NewSubnetConversion creates a new *SubnetConversion and initializes it.
func NewSubnetConversion(subnetConversionID ids.ID) (*SubnetConversion, error) {
	sc := &SubnetConversion{
		SubnetConversionID: subnetConversionID,
	}
	return sc, initialize(sc)
}

// ParseSubnetConversion converts a slice of bytes into an initialized SubnetConversion.
func ParseSubnetConversion(b []byte) (*SubnetConversion, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetConversion)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewSubnetConversion or Parse.
func (b *SubnetConversion) Bytes() []byte {
	return b.bytes
}

func (b *SubnetConversion) initialize(bytes []byte) {
	b.bytes = bytes
}
