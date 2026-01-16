// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

var junkBytes = []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}

func TestParseJunk(t *testing.T) {
	require := require.New(t)
	_, err := Parse(junkBytes)
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

func TestParseWrongPayloadType(t *testing.T) {
	require := require.New(t)
	hashPayload, err := NewHash(ids.GenerateTestID())
	require.NoError(err)

	shortID := ids.GenerateTestShortID()
	addressedPayload, err := NewAddressedCall(
		shortID[:],
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	_, err = ParseAddressedCall(hashPayload.Bytes())
	require.ErrorIs(err, ErrWrongType)

	_, err = ParseHash(addressedPayload.Bytes())
	require.ErrorIs(err, ErrWrongType)
}

func TestParse(t *testing.T) {
	require := require.New(t)
	hashPayload, err := NewHash(ids.ID{4, 5, 6})
	require.NoError(err)

	parsedHashPayload, err := Parse(hashPayload.Bytes())
	require.NoError(err)
	require.Equal(hashPayload, parsedHashPayload)

	addressedPayload, err := NewAddressedCall(
		[]byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		[]byte{10, 11, 12},
	)
	require.NoError(err)

	parsedAddressedPayload, err := Parse(addressedPayload.Bytes())
	require.NoError(err)
	require.Equal(addressedPayload, parsedAddressedPayload)
}
