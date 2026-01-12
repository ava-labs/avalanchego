// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

func TestAddressedCall(t *testing.T) {
	require := require.New(t)
	shortID := ids.GenerateTestShortID()

	addressedPayload, err := NewAddressedCall(
		shortID[:],
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	addressedPayloadBytes := addressedPayload.Bytes()
	parsedAddressedPayload, err := ParseAddressedCall(addressedPayloadBytes)
	require.NoError(err)
	require.Equal(addressedPayload, parsedAddressedPayload)
}

func TestParseAddressedCallJunk(t *testing.T) {
	_, err := ParseAddressedCall(junkBytes)
	require.ErrorIs(t, err, codec.ErrUnknownVersion)
}

func TestAddressedCallBytes(t *testing.T) {
	require := require.New(t)
	base64Payload := "AAAAAAABAAAAEAECAwAAAAAAAAAAAAAAAAAAAAADCgsM"
	addressedPayload, err := NewAddressedCall(
		[]byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		[]byte{10, 11, 12},
	)
	require.NoError(err)
	require.Equal(base64Payload, base64.StdEncoding.EncodeToString(addressedPayload.Bytes()))
}
