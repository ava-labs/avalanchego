// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/stretchr/testify/require"
)

func TestNewSummary(t *testing.T) {
	var (
		require            = require.New(t)
		blockID            = ids.GenerateTestID()
		blockNumber        = uint64(1337)
		rootID             = ids.GenerateTestID()
		calledOnAcceptFunc bool
		acceptFunc         = func(Summary) (block.StateSyncMode, error) {
			calledOnAcceptFunc = true
			return block.StateSyncStatic, nil
		}
	)

	summary, err := NewSummary(blockID, blockNumber, rootID, acceptFunc)
	require.NoError(err)

	require.NotEqual(ids.ID{}, summary.summaryID)
	require.Equal(summary.summaryID, summary.ID())
	require.Equal(blockID, summary.BlockID)
	require.Equal(blockNumber, summary.BlockHeight)
	require.Equal(blockNumber, summary.Height())
	require.Equal(rootID, summary.BlockRootID)
	require.Equal(summary.bytes, summary.Bytes())

	mode, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, mode)
	require.True(calledOnAcceptFunc)
}
