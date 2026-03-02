// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	timestamp := time.Now().Truncate(time.Second)
	parentID := ids.GenerateTestID()
	height := uint64(1337)
	proposalTx, err := testProposalTx()
	require.NoError(t, err)
	decisionTxs, err := testDecisionTxs()
	require.NoError(t, err)

	type test struct {
		name        string
		proposalTx  *txs.Tx
		decisionTxs []*txs.Tx
	}

	tests := []test{
		{
			name:        "no decision txs",
			proposalTx:  proposalTx,
			decisionTxs: []*txs.Tx{},
		},
		{
			name:        "decision txs",
			proposalTx:  proposalTx,
			decisionTxs: decisionTxs,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			blk, err := NewBanffProposalBlock(
				timestamp,
				parentID,
				height,
				test.proposalTx,
				test.decisionTxs,
			)
			require.NoError(err)

			require.NotEmpty(blk.Bytes())
			require.Equal(parentID, blk.Parent())
			require.Equal(height, blk.Height())
			require.Equal(timestamp, blk.Timestamp())

			l := len(test.decisionTxs)
			expectedTxs := make([]*txs.Tx, l+1)
			copy(expectedTxs, test.decisionTxs)
			expectedTxs[l] = test.proposalTx

			blkTxs := blk.Txs()
			require.Equal(expectedTxs, blkTxs)
			for i, blkTx := range blkTxs {
				expectedTx := expectedTxs[i]
				require.NotEmpty(blkTx.Bytes())
				require.NotEqual(ids.Empty, blkTx.ID())
				require.Equal(expectedTx.Bytes(), blkTx.Bytes())
			}
		})
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

	expectedTxs := []*txs.Tx{proposalTx}

	blkTxs := blk.Txs()
	require.Equal(expectedTxs, blkTxs)
	for i, blkTx := range blkTxs {
		expectedTx := expectedTxs[i]
		require.NotEmpty(blkTx.Bytes())
		require.NotEqual(ids.Empty, blkTx.ID())
		require.Equal(expectedTx.Bytes(), blkTx.Bytes())
	}
}
