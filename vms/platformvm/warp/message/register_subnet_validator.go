// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

// RegisterSubnetValidator adds a validator to the subnet.
type RegisterSubnetValidator struct {
	payload

	SubnetID ids.ID `serialize:"true" json:"subnetID"`
	// TODO: Use a 32-byte nodeID here
	NodeID       ids.NodeID             `serialize:"true" json:"nodeID"`
	Weight       uint64                 `serialize:"true" json:"weight"`
	BLSPublicKey [bls.PublicKeyLen]byte `serialize:"true" json:"blsPublicKey"`
	Expiry       uint64                 `serialize:"true" json:"expiry"`
}

// NewRegisterSubnetValidator creates a new initialized RegisterSubnetValidator.
func NewRegisterSubnetValidator(
	subnetID ids.ID,
	nodeID ids.NodeID,
	weight uint64,
	blsPublicKey [bls.PublicKeyLen]byte,
	expiry uint64,
) (*RegisterSubnetValidator, error) {
	msg := &RegisterSubnetValidator{
		SubnetID:     subnetID,
		NodeID:       nodeID,
		Weight:       weight,
		BLSPublicKey: blsPublicKey,
		Expiry:       expiry,
	}
	return msg, initialize(msg)
}

// ParseRegisterSubnetValidator parses bytes into an initialized
// RegisterSubnetValidator.
func ParseRegisterSubnetValidator(b []byte) (*RegisterSubnetValidator, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*RegisterSubnetValidator)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
