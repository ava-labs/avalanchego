// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func TestBlockSyncSummaryParser_ParseValid(t *testing.T) {
	t.Parallel()

	blockSyncSummary, err := NewBlockSyncSummary(common.Hash{1}, 2, common.Hash{3})
	require.NoError(t, err)

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

	mode, err := s.Accept(context.Background())
	require.NoError(t, err)
	require.Equal(t, block.StateSyncSkipped, mode)
	require.True(t, called)
}

func TestBlockSyncSummaryParser_ParseInvalid(t *testing.T) {
	t.Parallel()

	parser := NewBlockSyncSummaryParser()
	_, err := parser.Parse([]byte("not-a-summary"), nil)
	require.ErrorIs(t, err, errInvalidBlockSyncSummary)
}
