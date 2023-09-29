// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"

	"github.com/stretchr/testify/require"
)

func TestAddressedPayload(t *testing.T) {
	require := require.New(t)
	shortID := ids.GenerateTestShortID()

	addressedPayload, err := NewAddressedPayload(
		shortID[:],
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	addressedPayloadBytes := addressedPayload.Bytes()
	addressedPayload2, err := ParseAddressedPayload(addressedPayloadBytes)
	require.NoError(err)
	require.Equal(addressedPayload, addressedPayload2)
}

func TestParseAddressedPayloadJunk(t *testing.T) {
	require := require.New(t)
	_, err := ParseAddressedPayload(utils.RandomBytes(1024))
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

func TestParseAddressedPayload(t *testing.T) {
	base64Payload := "AAAAAAAAAAAAEAECAwAAAAAAAAAAAAAAAAAAAAADCgsM"
	payload := &AddressedPayload{
		SourceAddress: []byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Payload:       []byte{10, 11, 12},
	}

	require.NoError(t, payload.initialize())

	require.Equal(t, base64Payload, base64.StdEncoding.EncodeToString(payload.Bytes()))

	parsedPayload, err := ParseAddressedPayload(payload.Bytes())
	require.NoError(t, err)
	require.Equal(t, payload, parsedPayload)
}

func TestBlockHashPayload(t *testing.T) {
	require := require.New(t)

	blockHashPayload, err := NewBlockHashPayload(ids.GenerateTestID())
	require.NoError(err)

	blockHashPayloadBytes := blockHashPayload.Bytes()
	blockHashPayload2, err := ParseBlockHashPayload(blockHashPayloadBytes)
	require.NoError(err)
	require.Equal(blockHashPayload, blockHashPayload2)
}

func TestParseBlockHashPayloadJunk(t *testing.T) {
	require := require.New(t)
	_, err := ParseBlockHashPayload(utils.RandomBytes(1024))
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

func TestParseBlockHashPayload(t *testing.T) {
	base64Payload := "AAAAAAABBAUGAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	payload := &BlockHashPayload{
		BlockHash: ids.ID{4, 5, 6},
	}

	require.NoError(t, payload.initialize())

	require.Equal(t, base64Payload, base64.StdEncoding.EncodeToString(payload.Bytes()))

	parsedPayload, err := ParseBlockHashPayload(payload.Bytes())
	require.NoError(t, err)
	require.Equal(t, payload, parsedPayload)
}

func TestParseWrongPayloadType(t *testing.T) {
	require := require.New(t)
	blockHashPayload, err := NewBlockHashPayload(ids.GenerateTestID())
	require.NoError(err)

	shortID := ids.GenerateTestShortID()
	addressedPayload, err := NewAddressedPayload(
		shortID[:],
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	_, err = ParseAddressedPayload(blockHashPayload.Bytes())
	require.ErrorIs(err, errWrongType)

	_, err = ParseBlockHashPayload(addressedPayload.Bytes())
	require.ErrorIs(err, errWrongType)
}
