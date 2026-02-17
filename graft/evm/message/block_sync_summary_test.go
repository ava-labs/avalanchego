// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message_test

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/message/messagetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func TestMarshalBlockSyncSummary(t *testing.T) {
	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		blockSyncSummary, err := message.NewBlockSyncSummary(c, common.Hash{1}, 2, common.Hash{3})
		require.NoError(t, err)

		require.Equal(t, common.Hash{1}, blockSyncSummary.GetBlockHash())
		require.Equal(t, uint64(2), blockSyncSummary.Height())
		require.Equal(t, common.Hash{3}, blockSyncSummary.GetBlockRoot())

		expectedBase64Bytes := "AAAAAAAAAAAAAgEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		require.Equal(t, expectedBase64Bytes, base64.StdEncoding.EncodeToString(blockSyncSummary.Bytes()))

		provider := message.NewBlockSyncSummaryProvider(c)
		called := false
		acceptImplTest := func(message.Syncable) (block.StateSyncMode, error) {
			called = true
			return block.StateSyncSkipped, nil
		}
		s, err := provider.Parse(blockSyncSummary.Bytes(), acceptImplTest)
		require.NoError(t, err)
		require.Equal(t, blockSyncSummary.GetBlockHash(), s.GetBlockHash())
		require.Equal(t, blockSyncSummary.Height(), s.Height())
		require.Equal(t, blockSyncSummary.GetBlockRoot(), s.GetBlockRoot())
		require.Equal(t, blockSyncSummary.Bytes(), s.Bytes())

		mode, err := s.Accept(t.Context())
		require.NoError(t, err)
		require.Equal(t, block.StateSyncSkipped, mode)
		require.True(t, called)
	})
}
