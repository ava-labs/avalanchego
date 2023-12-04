// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestNewBanffProposalBlock(t *testing.T) {
	require := require.New(t)

	timestamp := time.Now().Truncate(time.Second)
	parentID := ids.GenerateTestID()
	height := uint64(1337)
	proposalTx, err := testProposalTx()
	require.NoError(err)

	blk, err := NewBanffProposalBlock(
		timestamp,
		parentID,
		height,
		proposalTx,
		[]*txs.Tx{},
	)
	require.NoError(err)

	require.NotEmpty(blk.Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())
	require.Equal(timestamp, blk.Timestamp())

	blkTxs := blk.Txs()
	require.Len(blkTxs, 1)
	expectedTxs := blk.Transactions
	expectedTxs = append(expectedTxs, blk.Tx)
	for i, blkTx := range blkTxs {
		expectedTx := expectedTxs[i]
		require.NotEmpty(blkTx.Bytes())
		require.NotEqual(ids.Empty, blkTx.ID())
		require.Equal(expectedTx.Bytes(), blkTx.Bytes())
	}
}

func TestNewBanffProposalBlockWithDecisionTxs(t *testing.T) {
	require := require.New(t)

	timestamp := time.Now().Truncate(time.Second)
	parentID := ids.GenerateTestID()
	height := uint64(1337)
	proposalTx, err := testProposalTx()
	require.NoError(err)
	decisionTxs, err := testDecisionTxs()
	require.NoError(err)

	blk, err := NewBanffProposalBlock(
		timestamp,
		parentID,
		height,
		proposalTx,
		decisionTxs,
	)
	require.NoError(err)

	require.NotEmpty(blk.Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())
	require.Equal(timestamp, blk.Timestamp())

	blkTxs := blk.Txs()
	require.Len(blkTxs, len(decisionTxs)+1)
	expectedTxs := blk.Transactions
	expectedTxs = append(expectedTxs, blk.Tx)
	for i, blkTx := range blkTxs {
		expectedTx := expectedTxs[i]
		require.NotEmpty(blkTx.Bytes())
		require.NotEqual(ids.Empty, blkTx.ID())
		require.Equal(expectedTx.Bytes(), blkTx.Bytes())
	}
}

func TestNewApricotProposalBlock(t *testing.T) {
	require := require.New(t)

	parentID := ids.GenerateTestID()
	height := uint64(1337)
	proposalTx, err := testProposalTx()
	require.NoError(err)

	blk, err := NewApricotProposalBlock(
		parentID,
		height,
		proposalTx,
	)
	require.NoError(err)

	require.NotEmpty(blk.Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())

	blkTxs := blk.Txs()
	require.Len(blkTxs, 1)
	expectedTxs := []*txs.Tx{proposalTx}
	for i, blkTx := range blkTxs {
		expectedTx := expectedTxs[i]
		require.NotEmpty(blkTx.Bytes())
		require.NotEqual(ids.Empty, blkTx.ID())
		require.Equal(expectedTx.Bytes(), blkTx.Bytes())
	}
}
