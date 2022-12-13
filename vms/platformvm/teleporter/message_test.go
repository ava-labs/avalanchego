// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
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

	_, err := ParseMessage(utils.RandomBytes(1024))
	require.Error(err)
}
