// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
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
	addressedPayload2, err := ParseAddressedCall(addressedPayloadBytes)
	require.NoError(err)
	require.Equal(addressedPayload, addressedPayload2)
}

func TestParseAddressedCallJunk(t *testing.T) {
	require := require.New(t)
	_, err := ParseAddressedCall(utils.RandomBytes(1024))
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

func TestParseAddressedCall(t *testing.T) {
	require := require.New(t)
	base64Payload := "AAAAAAAAAAAAEAECAwAAAAAAAAAAAAAAAAAAAAADCgsM"
	payload := &AddressedCall{
		SourceAddress: []byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Payload:       []byte{10, 11, 12},
	}

	require.NoError(initialize(payload))

	require.Equal(base64Payload, base64.StdEncoding.EncodeToString(payload.Bytes()))

	parsedPayload, err := ParseAddressedCall(payload.Bytes())
	require.NoError(err)
	require.Equal(payload, parsedPayload)
}

func TestBlockHash(t *testing.T) {
	require := require.New(t)

	blockHashPayload, err := NewBlockHash(ids.GenerateTestID())
	require.NoError(err)

	blockHashPayloadBytes := blockHashPayload.Bytes()
	blockHashPayload2, err := ParseBlockHash(blockHashPayloadBytes)
	require.NoError(err)
	require.Equal(blockHashPayload, blockHashPayload2)
}

func TestParseBlockHashJunk(t *testing.T) {
	require := require.New(t)
	_, err := ParseBlockHash(utils.RandomBytes(1024))
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

func TestParseBlockHash(t *testing.T) {
	require := require.New(t)
	base64Payload := "AAAAAAABBAUGAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	payload := &BlockHash{
		BlockHash: ids.ID{4, 5, 6},
	}

	require.NoError(initialize(payload))

	require.Equal(base64Payload, base64.StdEncoding.EncodeToString(payload.Bytes()))

	parsedPayload, err := ParseBlockHash(payload.Bytes())
	require.NoError(err)
	require.Equal(payload, parsedPayload)
}

func TestParseWrongPayloadType(t *testing.T) {
	require := require.New(t)
	blockHashPayload, err := NewBlockHash(ids.GenerateTestID())
	require.NoError(err)

	shortID := ids.GenerateTestShortID()
	addressedPayload, err := NewAddressedCall(
		shortID[:],
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	_, err = ParseAddressedCall(blockHashPayload.Bytes())
	require.ErrorIs(err, errWrongType)

	_, err = ParseBlockHash(addressedPayload.Bytes())
	require.ErrorIs(err, errWrongType)
}

func TestParseJunk(t *testing.T) {
	require := require.New(t)
	_, err := Parse(utils.RandomBytes(1024))
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

func TestParsePayload(t *testing.T) {
	require := require.New(t)
	base64BlockHashPayload := "AAAAAAABBAUGAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	blockHashPayload := &BlockHash{
		BlockHash: ids.ID{4, 5, 6},
	}

	require.NoError(initialize(blockHashPayload))

	require.Equal(base64BlockHashPayload, base64.StdEncoding.EncodeToString(blockHashPayload.Bytes()))

	parsedBlockHashPayload, err := Parse(blockHashPayload.Bytes())
	require.NoError(err)
	require.Equal(blockHashPayload, parsedBlockHashPayload)

	base64AddressedPayload := "AAAAAAAAAAAAEAECAwAAAAAAAAAAAAAAAAAAAAADCgsM"
	addressedPayload := &AddressedCall{
		SourceAddress: []byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Payload:       []byte{10, 11, 12},
	}

	require.NoError(initialize(addressedPayload))

	require.Equal(base64AddressedPayload, base64.StdEncoding.EncodeToString(addressedPayload.Bytes()))

	parsedAddressedPayload, err := Parse(addressedPayload.Bytes())
	require.NoError(err)
	require.Equal(addressedPayload, parsedAddressedPayload)
}
