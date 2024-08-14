// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"
)

var errWrongType = errors.New("wrong payload type")

type Message interface {
	Bytes() []byte

	initialize(b []byte)
}

func Parse(bytes []byte) (Message, error) {
	var payload Message
	if _, err := Codec.Unmarshal(bytes, &payload); err != nil {
		return nil, err
	}
	payload.initialize(bytes)
	return payload, nil
}

func initialize(p Message) error {
	bytes, err := Codec.Marshal(CodecVersion, &p)
	if err != nil {
		return fmt.Errorf("couldn't marshal %T payload: %w", p, err)
	}
	p.initialize(bytes)
	return nil
}
