// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	pb "github.com/ava-labs/avalanchego/proto/pb/message"
)

var ErrUnknownMessageType = errors.New("unknown message type")

func ParseTx(bytes []byte) ([]byte, error) {
	var protoMsg pb.Message
	if err := proto.Unmarshal(bytes, &protoMsg); err != nil {
		return nil, err
	}

	switch m := protoMsg.GetMessage().(type) {
	case *pb.Message_Tx:
		return m.Tx.Tx, nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnknownMessageType, protoMsg.GetMessage())
	}
}

func BuildTx(txBytes []byte) ([]byte, error) {
	return proto.Marshal(&pb.Message{
		Message: &pb.Message_Tx{
			Tx: &pb.Tx{
				Tx: txBytes,
			},
		},
	})
}
