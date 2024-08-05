// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SetSubnetValidatorManager is signed when the subnet wants to set the manager
// address of the subnet.
type SetSubnetValidatorManager struct {
	SubnetID ids.ID `serialize:"true"`
	ChainID  ids.ID `serialize:"true"`
	Address  []byte `serialize:"true"`

	bytes []byte
}

// NewSetSubnetValidatorManager creates a new *SetSubnetValidatorManager and initializes it.
func NewSetSubnetValidatorManager(subnetID ids.ID, chainID ids.ID, address []byte) (*SetSubnetValidatorManager, error) {
	bhp := &SetSubnetValidatorManager{
		SubnetID: subnetID,
		ChainID:  chainID,
		Address:  address,
	}
	return bhp, initialize(bhp)
}

// ParseSetSubnetValidatorManager converts a slice of bytes into an initialized SetSubnetValidatorManager.
func ParseSetSubnetValidatorManager(b []byte) (*SetSubnetValidatorManager, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SetSubnetValidatorManager)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewSetSubnetValidatorManager or Parse.
func (b *SetSubnetValidatorManager) Bytes() []byte {
	return b.bytes
}

func (b *SetSubnetValidatorManager) initialize(bytes []byte) {
	b.bytes = bytes
}
