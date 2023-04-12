// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"

	pbmessage "github.com/ava-labs/avalanchego/proto/pb/message"
)

var (
	_ Message = (*Tx)(nil)

	ErrUnexpectedCodecVersion = errors.New("unexpected codec version")
	errUnknownMessageType     = errors.New("unknown message type")
)

type Message interface {
	// Handle this message with the correct message handler
	Handle(handler Handler, nodeID ids.NodeID, requestID uint32) error

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

func Parse(bytes []byte) (Message, error) {
	var (
		msg      Message
		protoMsg pbmessage.Message
	)

	if err := proto.Unmarshal(bytes, &protoMsg); err == nil {
		// This message was encoded with proto.
		switch m := protoMsg.GetMessage().(type) {
		case *pbmessage.Message_Tx:
			msg = &Tx{
				Tx: m.Tx.TxBytes,
			}
		default:
			return nil, fmt.Errorf("%w: %T", errUnknownMessageType, protoMsg.GetMessage())
		}
	} else {
		// This message wasn't encoded with proto.
		// It must have been encoded with avalanchego's codec.
		// TODO remove else statement remove once all nodes support proto encoding.
		// i.e. when all nodes are on v1.11.0 or later.
		version, err := c.Unmarshal(bytes, &msg)
		if err != nil {
			return nil, err
		}
		if version != codecVersion {
			return nil, ErrUnexpectedCodecVersion
		}
	}
	msg.initialize(bytes)
	return msg, nil
}

func Build(msg Message) ([]byte, error) {
	bytes, err := c.Marshal(codecVersion, &msg)
	msg.initialize(bytes)
	return bytes, err
}

// TODO once all nodes support handling of proto encoded messages
// (i.e. when all nodes are on v1.11.0 or later), replace Build
// with this function.
func BuildProto(msg Message) ([]byte, error) {
	var protoMsg pbmessage.Message
	switch m := msg.(type) {
	case *Tx:
		protoMsg.Message = &pbmessage.Message_Tx{
			Tx: &pbmessage.Tx{
				TxBytes: m.Tx,
			},
		}
	default:
		return nil, fmt.Errorf("%w: %T", errUnknownMessageType, msg)
	}
	bytes, err := proto.Marshal(&protoMsg)
	msg.initialize(bytes)
	return bytes, err
}
