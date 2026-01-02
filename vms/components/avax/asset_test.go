// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
)

func TestAssetVerifyNil(t *testing.T) {
	id := (*Asset)(nil)
	err := id.Verify()
	require.ErrorIs(t, err, errNilAssetID)
}

func TestAssetVerifyEmpty(t *testing.T) {
	id := Asset{}
	err := id.Verify()
	require.ErrorIs(t, err, errEmptyAssetID)
}

func TestAssetID(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(codecVersion, c))

	id := Asset{
		ID: ids.ID{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
			0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
			0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		},
	}

	require.NoError(id.Verify())

	bytes, err := manager.Marshal(codecVersion, &id)
	require.NoError(err)

	newID := Asset{}
	_, err = manager.Unmarshal(bytes, &newID)
	require.NoError(err)

	require.NoError(newID.Verify())

	require.Equal(id.AssetID(), newID.AssetID())
}
