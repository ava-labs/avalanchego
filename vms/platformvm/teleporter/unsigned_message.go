// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"github.com/ava-labs/avalanchego/ids"
)

// UnsignedMessage defines the standard format for an unsigned Teleporter
// cross-subnet message.
type UnsignedMessage struct {
	SourceChainID      ids.ID `serialize:"true"`
	DestinationChainID ids.ID `serialize:"true"`
	Payload            []byte `serialize:"true"`

	bytes []byte
}

// NewUnsignedMessage creates a new *UnsignedMessage and initializes it.
func NewUnsignedMessage(
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	payload []byte,
) (*UnsignedMessage, error) {
	msg := &UnsignedMessage{
		SourceChainID:      sourceChainID,
		DestinationChainID: destinationChainID,
		Payload:            payload,
	}
	return msg, msg.Initialize()
}

// ParseUnsignedMessage converts a slice of bytes into an initialized
// *UnsignedMessage.
func ParseUnsignedMessage(b []byte) (*UnsignedMessage, error) {
	msg := &UnsignedMessage{
		bytes: b,
	}
	_, err := c.Unmarshal(b, msg)
	return msg, err
}

// Initialize recalculates the result of Bytes().
func (m *UnsignedMessage) Initialize() error {
	bytes, err := c.Marshal(codecVersion, m)
	m.bytes = bytes
	return err
}

// Bytes returns the binary representation of this message. It assumes that the
// message is initialized from either New, Parse, or an explicit call to
// Initialize.
func (m *UnsignedMessage) Bytes() []byte {
	return m.bytes
}
