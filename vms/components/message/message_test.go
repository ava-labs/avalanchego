// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
)

func TestParseGibberish(t *testing.T) {
	randomBytes := []byte{0, 1, 2, 3, 4, 5}
	_, err := Parse(randomBytes)
	require.ErrorIs(t, err, codec.ErrUnknownVersion)
}

func TestBuildParseProto(t *testing.T) {
	require := require.New(t)

	txBytes := []byte{'y', 'e', 'e', 't'}
	txMsg := Tx{
		Tx: txBytes,
	}
	txMsgBytes, err := BuildProto(&txMsg)
	require.NoError(err)

	parsedMsgIntf, err := Parse(txMsgBytes)
	require.NoError(err)

	parsedMsg, ok := parsedMsgIntf.(*Tx)
	require.True(ok)

	require.Equal(txBytes, parsedMsg.Tx)

	// Parse invalid message
	_, err = Parse([]byte{1, 3, 3, 7})
	require.Error(err)
}
