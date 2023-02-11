// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

// Message defines the standard format for a Warp message.
type Message struct {
	UnsignedMessage `serialize:"true"`
	Signature       Signature `serialize:"true"`

	bytes []byte
}

// NewMessage creates a new *Message and initializes it.
func NewMessage(
	unsignedMsg *UnsignedMessage,
	signature Signature,
) (*Message, error) {
	msg := &Message{
		UnsignedMessage: *unsignedMsg,
		Signature:       signature,
	}
	return msg, msg.Initialize()
}

// ParseMessage converts a slice of bytes into an initialized *Message.
func ParseMessage(b []byte) (*Message, error) {
	msg := &Message{
		bytes: b,
	}
	_, err := c.Unmarshal(b, msg)
	if err != nil {
		return nil, err
	}
	return msg, msg.UnsignedMessage.Initialize()
}

// Initialize recalculates the result of Bytes(). It does not call Initialize()
// on the UnsignedMessage.
func (m *Message) Initialize() error {
	bytes, err := c.Marshal(codecVersion, m)
	m.bytes = bytes
	return err
}

// Bytes returns the binary representation of this message. It assumes that the
// message is initialized from either New, Parse, or an explicit call to
// Initialize.
func (m *Message) Bytes() []byte {
	return m.bytes
}
