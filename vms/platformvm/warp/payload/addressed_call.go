// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import "fmt"

var _ Payload = (*AddressedCall)(nil)

// AddressedCall defines the format for delivering a call across VMs including a
// source address and a payload.
//
// Note: If a destination address is expected, it should be encoded in the
// payload.
type AddressedCall struct {
	SourceAddress []byte `serialize:"true"`
	Payload       []byte `serialize:"true"`

	bytes []byte
}

// NewAddressedCall creates a new *AddressedCall and initializes it.
func NewAddressedCall(sourceAddress []byte, payload []byte) (*AddressedCall, error) {
	ap := &AddressedCall{
		SourceAddress: sourceAddress,
		Payload:       payload,
	}
	return ap, initialize(ap)
}

// ParseAddressedCall converts a slice of bytes into an initialized
// AddressedCall.
func ParseAddressedCall(b []byte) (*AddressedCall, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*AddressedCall)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewAddressedCall or Parse.
func (a *AddressedCall) Bytes() []byte {
	return a.bytes
}

func (a *AddressedCall) initialize(bytes []byte) {
	a.bytes = bytes
}
