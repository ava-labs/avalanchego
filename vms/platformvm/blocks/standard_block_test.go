// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestNewBlueberryStandardBlock(t *testing.T) {
	require := require.New(t)

	blk, err := NewBlueberryStandardBlock(
		time.Now(),
		ids.GenerateTestID(),
		1337,
		[]*txs.Tx{
			{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins:  []*avax.TransferableInput{},
							Outs: []*avax.TransferableOutput{},
						},
					},
					Stake:     []*avax.TransferableOutput{},
					Validator: validator.Validator{},
					RewardsOwner: &secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{},
					},
				},
				Creds: []verify.Verifiable{},
			},
		},
	)
	require.NoError(err)

	// Make sure the block and tx are initialized
	require.NotNil(blk.Bytes())
	require.NotNil(blk.Transactions[0].Bytes())
	require.NotEqual(ids.Empty, blk.Transactions[0].ID())
}

func TestNewApricotStandardBlock(t *testing.T) {
	require := require.New(t)

	blk, err := NewApricotStandardBlock(
		ids.GenerateTestID(),
		1337,
		[]*txs.Tx{
			{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins:  []*avax.TransferableInput{},
							Outs: []*avax.TransferableOutput{},
						},
					},
					Stake:     []*avax.TransferableOutput{},
					Validator: validator.Validator{},
					RewardsOwner: &secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{},
					},
				},
				Creds: []verify.Verifiable{},
			},
		},
	)
	require.NoError(err)

	// Make sure the block and tx are initialized
	require.NotNil(blk.Bytes())
	require.NotNil(blk.Transactions[0].Bytes())
	require.NotEqual(ids.Empty, blk.Transactions[0].ID())
}
