// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"
)

var ErrWrongType = errors.New("wrong payload type")

// Payload provides a common interface for all payloads implemented by this
// package.
type Payload interface {
	// Bytes returns the binary representation of this payload.
	//
	// If the payload is not initialized, this method will return nil.
	Bytes() []byte

	// initialize the payload with the provided binary representation.
	initialize(b []byte)
}

// payload is embedded by all the payloads to provide the common implementation
// of Payload.
type payload []byte

func (p payload) Bytes() []byte {
	return p
}

func (p *payload) initialize(bytes []byte) {
	*p = bytes
}

func Parse(bytes []byte) (Payload, error) {
	var p Payload
	if _, err := Codec.Unmarshal(bytes, &p); err != nil {
		return nil, err
	}
	p.initialize(bytes)
	return p, nil
}

func Initialize(p Payload) error {
	bytes, err := Codec.Marshal(CodecVersion, &p)
	if err != nil {
		return fmt.Errorf("couldn't marshal %T payload: %w", p, err)
	}
	p.initialize(bytes)
	return nil
}
