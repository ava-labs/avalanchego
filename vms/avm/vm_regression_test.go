// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/upgrade/upgradetest"
	"github.com/MetalBlockchain/metalgo/vms/components/avax"
	"github.com/MetalBlockchain/metalgo/vms/components/verify"
	"github.com/MetalBlockchain/metalgo/vms/nftfx"
	"github.com/MetalBlockchain/metalgo/vms/secp256k1fx"
)

func TestVerifyFxUsage(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{fork: upgradetest.Latest})
	env.vm.ctx.Lock.Unlock()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)
	)

	initialStates := map[uint32][]verify.State{
		0: {
			&secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		},
		1: {
			&nftfx.MintOutput{
				GroupID: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		},
	}

	// Create the asset
	createAssetTx, err := env.txBuilder.CreateAssetTx(
		"Team Rocket", // name
		"TR",          // symbol
		0,             // denomination
		initialStates,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, createAssetTx)

	// Mint the NFT
	mintNFTTx, err := env.txBuilder.MintNFT(
		createAssetTx.ID(),
		[]byte{'h', 'e', 'l', 'l', 'o'}, // payload
		[]*secp256k1fx.OutputOwners{{
			Threshold: 1,
			Addrs:     []ids.ShortID{key.Address()},
		}},
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, mintNFTTx)

	// move the NFT
	to := keys[2].PublicKey().Address()
	spendTx, err := env.txBuilder.BaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		}},
		nil, // memo
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, spendTx)
}
