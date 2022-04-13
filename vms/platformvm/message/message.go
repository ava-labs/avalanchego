// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"

	"github.com/chain4travel/caminogo/ids"
)

var (
	_ Message = &Tx{}

	errUnexpectedCodecVersion = errors.New("unexpected codec version")
)

type Message interface {
	// Handle this message with the correct message handler
	Handle(handler Handler, nodeID ids.ShortID, requestID uint32) error

	// initialize should be called whenever a message is built or parsed
	initialize([]byte)

	// Bytes returns the binary representation of this message
	//
	// Bytes should only be called after being initialized
	Bytes() []byte
}

type message []byte

func (m *message) initialize(bytes []byte) { *m = bytes }
func (m *message) Bytes() []byte           { return *m }

type Tx struct {
	message

	Tx []byte `serialize:"true"`
}

func (msg *Tx) Handle(handler Handler, nodeID ids.ShortID, requestID uint32) error {
	return handler.HandleTx(nodeID, requestID, msg)
}

func Parse(bytes []byte) (Message, error) {
	var msg Message
	version, err := c.Unmarshal(bytes, &msg)
	if err != nil {
		return nil, err
	}
	if version != codecVersion {
		return nil, errUnexpectedCodecVersion
	}
	msg.initialize(bytes)
	return msg, nil
}

func Build(msg Message) ([]byte, error) {
	bytes, err := c.Marshal(codecVersion, &msg)
	msg.initialize(bytes)
	return bytes, err
}
