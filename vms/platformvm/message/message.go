// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ GossipMessage = (*TxGossip)(nil)

	errUnexpectedCodecVersion = errors.New("unexpected codec version")
)

type GossipMessage interface {
	// Handle this message with the correct message handler
	Handle(handler GossipHandler, nodeID ids.NodeID) error

	// initialize should be called whenever a message is built or parsed
	initialize([]byte)

	// Bytes returns the binary representation of this message
	//
	// Bytes should only be called after being initialized
	Bytes() []byte
}

type message []byte

func (m *message) initialize(bytes []byte) {
	*m = bytes
}

func (m *message) Bytes() []byte {
	return *m
}

type TxGossip struct {
	message

	Tx []byte `serialize:"true"`
}

func (msg *TxGossip) Handle(handler GossipHandler, nodeID ids.NodeID) error {
	return handler.HandleTx(nodeID, msg)
}

func Parse(bytes []byte) (GossipMessage, error) {
	var msg GossipMessage
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

func Build(msg GossipMessage) ([]byte, error) {
	bytes, err := c.Marshal(codecVersion, &msg)
	msg.initialize(bytes)
	return bytes, err
}
