// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func TestMarshalBlockSyncSummary(t *testing.T) {
	blockSyncSummary, err := NewBlockSyncSummary(common.Hash{1}, 2, common.Hash{3})
	require.NoError(t, err)

	require.Equal(t, common.Hash{1}, blockSyncSummary.GetBlockHash())
	require.Equal(t, uint64(2), blockSyncSummary.Height())
	require.Equal(t, common.Hash{3}, blockSyncSummary.GetBlockRoot())

	expectedBase64Bytes := "AAAAAAAAAAAAAgEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	require.Equal(t, expectedBase64Bytes, base64.StdEncoding.EncodeToString(blockSyncSummary.Bytes()))

	parser := NewBlockSyncSummaryParser()
	called := false
	acceptImplTest := func(Syncable) (block.StateSyncMode, error) {
		called = true
		return block.StateSyncSkipped, nil
	}
	s, err := parser.Parse(blockSyncSummary.Bytes(), acceptImplTest)
	require.NoError(t, err)
	require.Equal(t, blockSyncSummary.GetBlockHash(), s.GetBlockHash())
	require.Equal(t, blockSyncSummary.Height(), s.Height())
	require.Equal(t, blockSyncSummary.GetBlockRoot(), s.GetBlockRoot())
	require.Equal(t, blockSyncSummary.Bytes(), s.Bytes())

	mode, err := s.Accept(t.Context())
	require.NoError(t, err)
	require.Equal(t, block.StateSyncSkipped, mode)
	require.True(t, called)
}
