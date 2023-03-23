// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestVerifyFxUsage(t *testing.T) {
	require := require.New(t)
	vm := &VM{}
	ctx := NewContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	err := vm.Initialize(
		context.Background(),
		ctx,
		manager.NewMemDB(version.Semantic1_0_0),
		genesisBytes,
		nil,
		nil,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty.Prefix(0),
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(1),
				Fx: &nftfx.Fx{},
			},
		},
		nil,
	)
	require.NoError(err)
	vm.batchTimeout = 0

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	createAssetTx := &txs.Tx{Unsigned: &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		Name:         "Team Rocket",
		Symbol:       "TR",
		Denomination: 0,
		States: []*txs.InitialState{
			{
				FxIndex: 0,
				Outs: []verify.State{
					&secp256k1fx.TransferOutput{
						Amt: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
				},
			},
			{
				FxIndex: 1,
				Outs: []verify.State{
					&nftfx.MintOutput{
						GroupID: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
				},
			},
		},
	}}
	require.NoError(vm.parser.InitializeTx(createAssetTx))

	_, err = vm.IssueTx(createAssetTx.Bytes())
	require.NoError(err)

	mintNFTTx := &txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		Ops: []*txs.Operation{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*avax.UTXOID{{
				TxID:        createAssetTx.ID(),
				OutputIndex: 1,
			}},
			Op: &nftfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
				GroupID: 1,
				Payload: []byte{'h', 'e', 'l', 'l', 'o'},
				Outputs: []*secp256k1fx.OutputOwners{{}},
			},
		}},
	}}
	require.NoError(mintNFTTx.SignNFTFx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))

	_, err = vm.IssueTx(mintNFTTx.Bytes())
	require.NoError(err)

	spendTx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    constants.UnitTestID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        createAssetTx.ID(),
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: createAssetTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 1,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
			},
		}},
	}}}
	require.NoError(spendTx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))

	_, err = vm.IssueTx(spendTx.Bytes())
	require.NoError(err)
}
