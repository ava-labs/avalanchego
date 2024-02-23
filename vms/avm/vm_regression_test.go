// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestVerifyFxUsage(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{fork: durango})
	env.vm.ctx.Lock.Unlock()
	defer func() {
		env.vm.ctx.Lock.Lock()
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain()
	)
	kc.Add(key)

	initialStates := map[uint32][]verify.State{
		uint32(0): {
			&secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		},
		uint32(1): {
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
	createAssetTx, _, err := buildCreateAssetTx(
		env.service.txBuilderBackend,
		"Team Rocket", // name
		"TR",          // symbol
		0,             // denomination
		initialStates,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, env.issuer, createAssetTx)

	// Mint the NFT
	env.service.txBuilderBackend.ResetAddresses(kc.Addresses())
	mintNFTTx, err := mintNFT(
		env.service.txBuilderBackend,
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
	issueAndAccept(require, env.vm, env.issuer, mintNFTTx)

	// move the NFT
	to := keys[2].PublicKey().Address()
	env.service.txBuilderBackend.ResetAddresses(kc.Addresses())
	spendTx, _, err := buildBaseTx(
		env.service.txBuilderBackend,
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
	issueAndAccept(require, env.vm, env.issuer, spendTx)
}
