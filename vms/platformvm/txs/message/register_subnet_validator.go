// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Message = (*RegisterSubnetValidator)(nil)

type RegisterSubnetValidator struct {
	SubnetID  ids.ID     `serialize:"true"`
	NodeID    ids.NodeID `serialize:"true"`
	Weight    uint64     `serialize:"true"`
	BlsPubKey []byte     `serialize:"true"`
	Expiry    uint64     `serialize:"true"`

	bytes []byte
}

func NewRegisterSubnetValidator(
	subnetID ids.ID,
	nodeID ids.NodeID,
	weight uint64,
	blsPubKey []byte,
	expiry uint64,
) (*RegisterSubnetValidator, error) {
	msg := &RegisterSubnetValidator{
		SubnetID:  subnetID,
		NodeID:    nodeID,
		Weight:    weight,
		BlsPubKey: blsPubKey,
		Expiry:    expiry,
	}
	return msg, initialize(msg)
}

func ParseRegisterSubnetValidator(b []byte) (*RegisterSubnetValidator, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*RegisterSubnetValidator)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

func (r *RegisterSubnetValidator) Bytes() []byte {
	return r.bytes
}

func (r *RegisterSubnetValidator) initialize(bytes []byte) {
	r.bytes = bytes
}
