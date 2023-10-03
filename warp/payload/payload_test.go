// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestAddressedPayload(t *testing.T) {
	require := require.New(t)

	addressedPayload, err := NewAddressedPayload(
		common.Address(ids.GenerateTestShortID()),
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	addressedPayloadBytes := addressedPayload.Bytes()
	addressedPayload2, err := ParseAddressedPayload(addressedPayloadBytes)
	require.NoError(err)
	require.Equal(addressedPayload, addressedPayload2)
}

func TestParseAddressedPayloadJunk(t *testing.T) {
	_, err := ParseAddressedPayload(utils.RandomBytes(1024))
	require.Error(t, err)
}

func TestParseAddressedPayload(t *testing.T) {
	base64Payload := "AAAAAAAAAQIDAAAAAAAAAAAAAAAAAAAAAAAAAAADCgsM"
	payload := &AddressedPayload{
		SourceAddress: common.Address{1, 2, 3},
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

	blockHashPayload, err := NewBlockHashPayload(common.Hash(ids.GenerateTestID()))
	require.NoError(err)

	blockHashPayloadBytes := blockHashPayload.Bytes()
	blockHashPayload2, err := ParseBlockHashPayload(blockHashPayloadBytes)
	require.NoError(err)
	require.Equal(blockHashPayload, blockHashPayload2)
}

func TestParseBlockHashPayloadJunk(t *testing.T) {
	_, err := ParseBlockHashPayload(utils.RandomBytes(1024))
	require.Error(t, err)
}

func TestParseBlockHashPayload(t *testing.T) {
	base64Payload := "AAAAAAABBAUGAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	payload := &BlockHashPayload{
		BlockHash: common.Hash{4, 5, 6},
	}

	require.NoError(t, payload.initialize())

	require.Equal(t, base64Payload, base64.StdEncoding.EncodeToString(payload.Bytes()))

	parsedPayload, err := ParseBlockHashPayload(payload.Bytes())
	require.NoError(t, err)
	require.Equal(t, payload, parsedPayload)
}

func TestParseWrongPayloadType(t *testing.T) {
	require := require.New(t)
	blockHashPayload, err := NewBlockHashPayload(common.Hash(ids.GenerateTestID()))
	require.NoError(err)

	addressedPayload, err := NewAddressedPayload(
		common.Address(ids.GenerateTestShortID()),
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	_, err = ParseAddressedPayload(blockHashPayload.Bytes())
	require.ErrorIs(err, errWrongType)

	_, err = ParseBlockHashPayload(addressedPayload.Bytes())
	require.ErrorIs(err, errWrongType)
}
