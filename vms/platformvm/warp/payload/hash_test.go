// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

func TestHash(t *testing.T) {
	require := require.New(t)

	hashPayload, err := NewHash(ids.GenerateTestID())
	require.NoError(err)

	hashPayloadBytes := hashPayload.Bytes()
	parsedHashPayload, err := ParseHash(hashPayloadBytes)
	require.NoError(err)
	require.Equal(hashPayload, parsedHashPayload)
}

func TestParseHashJunk(t *testing.T) {
	_, err := ParseHash(junkBytes)
	require.ErrorIs(t, err, codec.ErrUnknownVersion)
}

func TestHashBytes(t *testing.T) {
	require := require.New(t)
	base64Payload := "AAAAAAAABAUGAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	hashPayload, err := NewHash(ids.ID{4, 5, 6})
	require.NoError(err)
	require.Equal(base64Payload, base64.StdEncoding.EncodeToString(hashPayload.Bytes()))
}
