// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestMessage(t *testing.T) {
	require := require.New(t)

	unsignedMsg, err := NewUnsignedMessage(
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		[]byte("payload"),
	)
	require.NoError(err)

	msg, err := NewMessage(
		unsignedMsg,
		&BitSetSignature{
			Signers:   []byte{1, 2, 3},
			Signature: [bls.SignatureLen]byte{4, 5, 6},
		},
	)
	require.NoError(err)

	msgBytes := msg.Bytes()
	msg2, err := ParseMessage(msgBytes)
	require.NoError(err)
	require.Equal(msg, msg2)
}

func TestParseMessageJunk(t *testing.T) {
	require := require.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	_, err := ParseMessage(bytes)
	require.ErrorIs(err, codec.ErrUnknownVersion)
}
