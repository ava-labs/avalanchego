// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func TestBlockSyncSummary_MarshalGolden(t *testing.T) {
	t.Parallel()

	blockSyncSummary, err := NewBlockSyncSummary(common.Hash{1}, 2, common.Hash{3})
	require.NoError(t, err)

	require.Equal(t, common.Hash{1}, blockSyncSummary.GetBlockHash())
	require.Equal(t, uint64(2), blockSyncSummary.Height())
	require.Equal(t, common.Hash{3}, blockSyncSummary.GetBlockRoot())

	expectedBase64Bytes := "AAAAAAAAAAAAAgEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	require.Equal(t, expectedBase64Bytes, base64.StdEncoding.EncodeToString(blockSyncSummary.Bytes()))
}
