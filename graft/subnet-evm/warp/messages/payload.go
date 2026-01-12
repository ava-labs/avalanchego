// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"errors"
	"fmt"
)

var errWrongType = errors.New("wrong payload type")

// Payload provides a common interface for all payloads implemented by this
// package.
type Payload interface {
	// Bytes returns the binary representation of this payload.
	Bytes() []byte

	// initialize the payload with the provided binary representation.
	initialize(b []byte)
}

func Parse(bytes []byte) (Payload, error) {
	var payload Payload
	if _, err := Codec.Unmarshal(bytes, &payload); err != nil {
		return nil, err
	}
	payload.initialize(bytes)
	return payload, nil
}

func initialize(p Payload) error {
	bytes, err := Codec.Marshal(CodecVersion, &p)
	if err != nil {
		return fmt.Errorf("couldn't marshal %T payload: %w", p, err)
	}
	p.initialize(bytes)
	return nil
}
