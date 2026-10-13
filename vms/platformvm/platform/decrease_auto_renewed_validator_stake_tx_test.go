// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platform

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

func TestDecreaseAutoRenewedValidatorStakeTxSyntacticVerify(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx
		want   error
	}{
		{
			name: "nil",
			mutate: func(*DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx {
				return nil
			},
			want: ErrNilTx,
		},
		{
			name: "already_verified",
			mutate: func(*DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx {
				return &DecreaseAutoRenewedValidatorStakeTx{
					BaseTx: BaseTx{
						SyntacticallyVerified: true,
					},
				}
			},
			want: nil,
		},
		{
			name: "empty_txID",
			mutate: func(tx *DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx {
				tx.TxID = ids.Empty
				return tx
			},
			want: errMissingTxID,
		},
		{
			name: "zero_amount",
			mutate: func(tx *DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx {
				tx.Amount = 0
				return tx
			},
			want: errZeroStakeDecrease,
		},
		{
			name: "invalid_BaseTx",
			mutate: func(tx *DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx {
				tx.BaseTx = BaseTx{}
				return tx
			},
			want: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid_auth",
			mutate: func(tx *DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx {
				tx.Auth = (*secp256k1fx.Input)(nil)
				return tx
			},
			want: secp256k1fx.ErrNilInput,
		},
		{
			name: "valid",
			mutate: func(tx *DecreaseAutoRenewedValidatorStakeTx) *DecreaseAutoRenewedValidatorStakeTx {
				return tx
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := snowtest.Context(t, snowtest.PChainID)

			tx := tt.mutate(&DecreaseAutoRenewedValidatorStakeTx{
				BaseTx: BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					},
				},
				TxID:   ids.GenerateTestID(),
				Auth:   &secp256k1fx.Input{SigIndices: []uint32{0}},
				Amount: units.Avax,
			})

			got := tx.SyntacticVerify(ctx)
			require.ErrorIs(t, got, tt.want)

			if tx != nil {
				require.Equal(t, tt.want == nil, tx.SyntacticallyVerified)
			}
		})
	}
}

func TestDecreaseAutoRenewedValidatorStakeTxSerialization(t *testing.T) {
	avaxAssetID, err := ids.FromString("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
	require.NoError(t, err)

	txID := ids.ID{
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}

	tx := &DecreaseAutoRenewedValidatorStakeTx{
		BaseTx: BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: constants.PlatformChainID,
				Outs:         []*avax.TransferableOutput{},
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        txID,
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 2 * units.KiloAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{1},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		TxID:   txID,
		Auth:   &secp256k1fx.Input{SigIndices: []uint32{1}},
		Amount: 500 * units.Avax,
	}
	avax.SortTransferableOutputs(tx.Outs, Codec)
	utils.Sort(tx.Ins)
	require.NoError(t, tx.SyntacticVerify(&snow.Context{
		NetworkID: 1,
		ChainID:   constants.PlatformChainID,
	}))

	wantBytes := []byte{
		// Codec version
		0x00, 0x00,
		// DecreaseAutoRenewedValidatorStakeTx type ID
		0x00, 0x00, 0x00, 0x2d,
		// Mainnet network ID
		0x00, 0x00, 0x00, 0x01,
		// P-chain blockchain ID
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Number of immediate outputs
		0x00, 0x00, 0x00, 0x00,
		// Number of inputs
		0x00, 0x00, 0x00, 0x01,
		// inputs[0]
		// TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// Tx output index
		0x00, 0x00, 0x00, 0x01,
		// Mainnet AVAX asset ID
		0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
		0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
		0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
		0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
		// secp256k1fx transfer input type ID
		0x00, 0x00, 0x00, 0x05,
		// Amount = 2k AVAX
		0x00, 0x00, 0x01, 0xd1, 0xa9, 0x4a, 0x20, 0x00,
		// Number of input signature indices
		0x00, 0x00, 0x00, 0x01,
		// signature index
		0x00, 0x00, 0x00, 0x01,
		// memo length
		0x00, 0x00, 0x00, 0x00,
		// Referenced auto-renewed validator TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// secp256k1fx input type ID (auth)
		0x00, 0x00, 0x00, 0x0a,
		// Number of auth signature indices
		0x00, 0x00, 0x00, 0x01,
		// auth signature index
		0x00, 0x00, 0x00, 0x01,
		// Amount = 500 AVAX
		0x00, 0x00, 0x00, 0x74, 0x6a, 0x52, 0x88, 0x00,
	}

	var unsignedTx UnsignedTx = tx
	gotBytes, err := Codec.Marshal(CodecVersion, &unsignedTx)
	require.NoError(t, err)
	require.Equal(t, wantBytes, gotBytes)
}
