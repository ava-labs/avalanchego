// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

func TestNewBanffStandardBlock(t *testing.T) {
	require := require.New(t)

	timestamp := time.Now().Truncate(time.Second)
	parentID := ids.GenerateTestID()
	height := uint64(1337)

	tx := &txs.Tx{
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
	require.NoError(tx.Initialize(txs.Codec))

	blk, err := NewBanffStandardBlock(
		timestamp,
		parentID,
		height,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	// Make sure the block and tx are initialized
	require.NotEmpty(blk.Bytes())
	require.NotEmpty(blk.Transactions[0].Bytes())
	require.NotEqual(ids.Empty, blk.Transactions[0].ID())
	require.Equal(tx.Bytes(), blk.Transactions[0].Bytes())
	require.Equal(timestamp, blk.Timestamp())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())
}

func TestNewApricotStandardBlock(t *testing.T) {
	require := require.New(t)

	parentID := ids.GenerateTestID()
	height := uint64(1337)

	tx := &txs.Tx{
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
	require.NoError(tx.Initialize(txs.Codec))

	blk, err := NewApricotStandardBlock(
		parentID,
		height,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	// Make sure the block and tx are initialized
	require.NotEmpty(blk.Bytes())
	require.NotEmpty(blk.Transactions[0].Bytes())
	require.NotEqual(ids.Empty, blk.Transactions[0].ID())
	require.Equal(tx.Bytes(), blk.Transactions[0].Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())
}
