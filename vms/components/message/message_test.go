// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/codec"

	pb "github.com/ava-labs/avalanchego/proto/pb/message"
)

func TestParseGibberish(t *testing.T) {
	randomBytes := []byte{0, 1, 2, 3, 4, 5}
	_, err := Parse(randomBytes)
	require.ErrorIs(t, err, codec.ErrUnknownVersion)
}

func TestParseProto(t *testing.T) {
	require := require.New(t)

	txBytes := []byte{'y', 'e', 'e', 't'}
	protoMsg := pb.Message{
		Message: &pb.Message_Tx{
			Tx: &pb.Tx{
				Tx: txBytes,
			},
		},
	}
	msgBytes, err := proto.Marshal(&protoMsg)
	require.NoError(err)

	parsedMsgIntf, err := Parse(msgBytes)
	require.NoError(err)

	require.IsType(&Tx{}, parsedMsgIntf)
	parsedMsg := parsedMsgIntf.(*Tx)

	require.Equal(txBytes, parsedMsg.Tx)

	// Parse invalid message
	_, err = Parse([]byte{1, 3, 3, 7})
	// Can't parse as proto so it falls back to using avalanchego's codec
	require.ErrorIs(err, codec.ErrUnknownVersion)
}
