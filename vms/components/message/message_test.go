// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"

	pb "github.com/ava-labs/avalanchego/proto/pb/message"
)

func TestParseGibberish(t *testing.T) {
	randomBytes := []byte{0, 1, 2, 3, 4, 5}
	_, err := ParseTx(randomBytes)
	require.ErrorIs(t, err, proto.Error)
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

	parsedTx, err := ParseTx(msgBytes)
	require.NoError(err)

	require.Equal(txBytes, parsedTx)

	// Parse invalid bytes
	_, err = ParseTx([]byte{1, 3, 3, 7})
	require.ErrorIs(err, proto.Error)
}

func TestTx(t *testing.T) {
	require := require.New(t)

	tx := utils.RandomBytes(256 * units.KiB)
	builtMsgBytes, err := BuildTx(tx)
	require.NoError(err)

	parsedTx, err := ParseTx(builtMsgBytes)
	require.NoError(err)
	require.Equal(tx, parsedTx)
}
