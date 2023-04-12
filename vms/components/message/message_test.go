// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

func TestParseGibberish(t *testing.T) {
	randomBytes := utils.RandomBytes(256 * units.KiB)
	_, err := Parse(randomBytes)
	require.Error(t, err)
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
