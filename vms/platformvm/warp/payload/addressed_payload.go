// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import "fmt"

var _ byteSetter = (*AddressedCall)(nil)

// AddressedCall defines the format for delivering a call across VMs including a
// source address and a payload.
// Implementation of destinationAddress can be implemented on top of this payload.
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
	return ap, ap.initialize()
}

// ParseAddressedCall converts a slice of bytes into an initialized
// AddressedCall.
func ParseAddressedCall(b []byte) (*AddressedCall, error) {
	var unmarshalledPayloadIntf any
	if _, err := c.Unmarshal(b, &unmarshalledPayloadIntf); err != nil {
		return nil, err
	}
	payload, ok := unmarshalledPayloadIntf.(*AddressedCall)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, unmarshalledPayloadIntf)
	}
	payload.bytes = b
	return payload, nil
}

// initialize recalculates the result of Bytes().
func (a *AddressedCall) initialize() error {
	payloadIntf := any(a)
	bytes, err := c.Marshal(codecVersion, &payloadIntf)
	if err != nil {
		return fmt.Errorf("couldn't marshal warp addressed payload: %w", err)
	}
	a.bytes = bytes
	return nil
}

func (a *AddressedCall) setBytes(bytes []byte) {
	a.bytes = bytes
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewAddressedCall or ParseAddressedCall.
func (a *AddressedCall) Bytes() []byte {
	return a.bytes
}
