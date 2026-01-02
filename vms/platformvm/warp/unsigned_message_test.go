// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestUnsignedMessage(t *testing.T) {
	require := require.New(t)

	msg, err := NewUnsignedMessage(
		constants.UnitTestID,
		ids.GenerateTestID(),
		[]byte("payload"),
	)
	require.NoError(err)

	msgBytes := msg.Bytes()
	msg2, err := ParseUnsignedMessage(msgBytes)
	require.NoError(err)
	require.Equal(msg, msg2)
}

func TestParseUnsignedMessageJunk(t *testing.T) {
	require := require.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	_, err := ParseUnsignedMessage(bytes)
	require.ErrorIs(err, codec.ErrUnknownVersion)
}
