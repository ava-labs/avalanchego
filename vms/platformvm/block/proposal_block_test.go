// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	timestamp = time.Now().Truncate(time.Second)
	parentID  = ids.GenerateTestID()
	height    = uint64(1337)

	tx = &txs.Tx{
		Unsigned: &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{
				BaseTx: avax.BaseTx{
					Ins:  []*avax.TransferableInput{},
					Outs: []*avax.TransferableOutput{},
				},
			},
			StakeOuts: []*avax.TransferableOutput{},
			Validator: txs.Validator{},
			RewardsOwner: &secp256k1fx.OutputOwners{
				Addrs: []ids.ShortID{},
			},
		},
		Creds: []verify.Verifiable{},
	}
)

func init() {
	if err := tx.Initialize(txs.Codec); err != nil {
		panic(err)
	}
}

func TestNewBanffProposalBlock(t *testing.T) {
	require := require.New(t)

	blk, err := NewBanffProposalBlock(
		timestamp,
		parentID,
		height,
		tx,
		[]*txs.Tx{},
	)
	require.NoError(err)

	require.NotEmpty(blk.Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())
	require.Equal(timestamp, blk.Timestamp())

	blkTxs := blk.Txs()
	require.Len(blkTxs, 1)
	for _, blkTx := range blkTxs {
		require.NotEmpty(blkTx.Bytes())
		require.NotEqual(ids.Empty, blkTx.ID())
		require.Equal(tx.Bytes(), blkTx.Bytes())
	}
}

func TestNewBanffProposalBlockWithDecisionTxs(t *testing.T) {
	require := require.New(t)

	blk, err := NewBanffProposalBlock(
		timestamp,
		parentID,
		height,
		tx,
		[]*txs.Tx{tx, tx, tx},
	)
	require.NoError(err)

	require.NotEmpty(blk.Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())
	require.Equal(timestamp, blk.Timestamp())

	blkTxs := blk.Txs()
	require.Len(blkTxs, 4)
	for _, blkTx := range blkTxs {
		require.NotEmpty(blkTx.Bytes())
		require.NotEqual(ids.Empty, blkTx.ID())
		require.Equal(tx.Bytes(), blkTx.Bytes())
	}
}

func TestNewApricotProposalBlock(t *testing.T) {
	require := require.New(t)

	blk, err := NewApricotProposalBlock(
		parentID,
		height,
		tx,
	)
	require.NoError(err)

	require.NotEmpty(blk.Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())

	for _, blkTx := range blk.Txs() {
		require.NotEmpty(blkTx.Bytes())
		require.NotEqual(ids.Empty, blkTx.ID())
		require.Equal(tx.Bytes(), blkTx.Bytes())
	}
}
