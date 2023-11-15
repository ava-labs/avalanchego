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

// Tests NewSummary, NewSummaryFromBytes and all the methods on Summary.
func TestSummary(t *testing.T) {
	var (
		require            = require.New(t)
		blockID            = ids.GenerateTestID()
		blockNumber        = uint64(1337)
		rootID             = ids.GenerateTestID()
		calledOnAcceptFunc int
		onAcceptFunc       = func(Summary) (block.StateSyncMode, error) {
			calledOnAcceptFunc++
			return block.StateSyncStatic, nil
		}
	)

	summary, err := NewSummary(blockID, blockNumber, rootID, onAcceptFunc)
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
	require.Equal(1, calledOnAcceptFunc)

	parsedSummary, err := NewSummaryFromBytes(summary.bytes, onAcceptFunc)
	require.NoError(err)

	// require.Equal(summary, parsedSummary) won't work because of onAcceptFunc.
	// Compare fields manually instead.
	require.Equal(summary.summaryID, parsedSummary.summaryID)
	require.Equal(summary.bytes, parsedSummary.bytes)
	require.Equal(summary.BlockID, parsedSummary.BlockID)
	require.Equal(summary.BlockHeight, parsedSummary.BlockHeight)

	mode, err = parsedSummary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, mode)
	require.Equal(2, calledOnAcceptFunc)
}
