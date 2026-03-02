// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestEffectiveGasPrice(t *testing.T) {
	avaxAssetID := ids.GenerateTestID()
	tests := []struct {
		name            string
		tx              UnsignedTx
		isApricotPhase5 bool
		want            uint256.Int
		wantErr         error
	}{
		{
			name:    "no_gas_used",
			tx:      &UnsignedImportTx{},
			wantErr: ErrNoGasUsed,
		},
		{
			name: "invalid_amount_burned",
			tx: &UnsignedImportTx{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						In: &secp256k1fx.TransferInput{
							Amt: 1,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
				Outs: []EVMOutput{
					{
						Amount:  units.NanoAvax,
						AssetID: avaxAssetID,
					},
				},
			},
			wantErr: math.ErrUnderflow,
		},
		{
			name: "valid",
			tx: &UnsignedImportTx{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: units.NanoAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
			},
			want: *uint256.NewInt(
				(units.NanoAvax * X2CRateUint64) / secp256k1fx.CostPerSignature,
			),
		},
		{
			name: "valid_multiple_assets",
			tx: &UnsignedImportTx{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: units.NanoAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
					{
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						In: &secp256k1fx.TransferInput{
							Amt: 1,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
			},
			want: *uint256.NewInt(
				(units.NanoAvax * X2CRateUint64) / (2 * secp256k1fx.CostPerSignature),
			),
		},
		{
			name: "valid_post_AP5",
			tx: &UnsignedImportTx{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: units.NanoAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
			},
			isApricotPhase5: true,
			want: *uint256.NewInt(
				(units.NanoAvax * X2CRateUint64) / (ap5.AtomicTxIntrinsicGas + secp256k1fx.CostPerSignature),
			),
		},
		{
			name: "gas_price_exceeds_uint64",
			tx: &UnsignedImportTx{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: units.MegaAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
			},
			want: *uint256.MustFromDecimal(
				"1000000000000000000000", // (units.MegaAvax * X2CRateUint64) / secp256k1fx.CostPerSignature,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			got, err := EffectiveGasPrice(test.tx, avaxAssetID, test.isApricotPhase5)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}
