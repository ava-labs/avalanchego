// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBuildDuplicateTxs(t *testing.T) {
	require := require.New(t)

	chainID := ids.ID{1}
	height := uint64(2)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{6}, {6}}
	_, err := Build(
		chainID,
		height,
		parentIDs,
		txs,
	)
	require.ErrorIs(err, errInvalidTxs)
}

func TestBuildValid(t *testing.T) {
	require := require.New(t)

	chainID := ids.ID{1}
	height := uint64(2)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{7}, {6}}
	vtx, err := Build(
		chainID,
		height,
		parentIDs,
		txs,
	)
	require.NoError(err)
	require.Equal(chainID, vtx.ChainID())
	require.Equal(height, vtx.Height())
	require.Equal(parentIDs, vtx.ParentIDs())
	require.Equal(txs, vtx.Txs())
}
