// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify/verifymock"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

func TestTransformSubnetTxSerialization(t *testing.T) {
	require := require.New(t)

	addr := ids.ShortID{
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
	}

	avaxAssetID, err := ids.FromString("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
	require.NoError(err)

	customAssetID := ids.ID{
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
	}

	txID := ids.ID{
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}
	subnetID := ids.ID{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
	}

	simpleTransformTx := &TransformSubnetTx{
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
							Amt: 10 * units.Avax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{5},
							},
						},
					},
					{
						UTXOID: avax.UTXOID{
							TxID:        txID,
							OutputIndex: 2,
						},
						Asset: avax.Asset{
							ID: customAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 0xefffffffffffffff,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		Subnet:                   subnetID,
		AssetID:                  customAssetID,
		InitialSupply:            0x1000000000000000,
		MaximumSupply:            0xffffffffffffffff,
		MinConsumptionRate:       1_000,
		MaxConsumptionRate:       1_000_000,
		MinValidatorStake:        1,
		MaxValidatorStake:        0xffffffffffffffff,
		MinStakeDuration:         1,
		MaxStakeDuration:         365 * 24 * 60 * 60,
		MinDelegationFee:         reward.PercentDenominator,
		MinDelegatorStake:        1,
		MaxValidatorWeightFactor: 1,
		UptimeRequirement:        .95 * reward.PercentDenominator,
		SubnetAuth: &secp256k1fx.Input{
			SigIndices: []uint32{3},
		},
	}
	require.NoError(simpleTransformTx.SyntacticVerify(&snow.Context{
		NetworkID:   1,
		ChainID:     constants.PlatformChainID,
		AVAXAssetID: avaxAssetID,
	}))

	expectedUnsignedSimpleTransformTxBytes := []byte{
		// Codec version
		0x00, 0x00,
		// TransformSubnetTx type ID
		0x00, 0x00, 0x00, 0x18,
		// Mainnet network ID
		0x00, 0x00, 0x00, 0x01,
		// P-chain blockchain ID
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// number of outputs
		0x00, 0x00, 0x00, 0x00,
		// number of inputs
		0x00, 0x00, 0x00, 0x02,
		// inputs[0]
		// TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// Tx output index
		0x00, 0x00, 0x00, 0x01,
		// Mainnet AVAX assetID
		0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
		0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
		0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
		0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
		// secp256k1fx transfer input type ID
		0x00, 0x00, 0x00, 0x05,
		// input amount = 10 AVAX
		0x00, 0x00, 0x00, 0x02, 0x54, 0x0b, 0xe4, 0x00,
		// number of signatures needed in input
		0x00, 0x00, 0x00, 0x01,
		// index of signer
		0x00, 0x00, 0x00, 0x05,
		// inputs[1]
		// TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// Tx output index
		0x00, 0x00, 0x00, 0x02,
		// custom asset ID
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		// secp256k1fx transfer input type ID
		0x00, 0x00, 0x00, 0x05,
		// input amount
		0xef, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		// number of signatures needed in input
		0x00, 0x00, 0x00, 0x01,
		// index of signer
		0x00, 0x00, 0x00, 0x00,
		// length of memo
		0x00, 0x00, 0x00, 0x00,
		// subnetID being transformed
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
		// staking asset ID
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		// initial supply
		0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// maximum supply
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		// minimum consumption rate
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0xe8,
		// maximum consumption rate
		0x00, 0x00, 0x00, 0x00, 0x00, 0x0f, 0x42, 0x40,
		// minimum staking amount
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		// maximum staking amount
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		// minimum staking duration
		0x00, 0x00, 0x00, 0x01,
		// maximum staking duration
		0x01, 0xe1, 0x33, 0x80,
		// minimum delegation fee
		0x00, 0x0f, 0x42, 0x40,
		// minimum delegation amount
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		// maximum validator weight factor
		0x01,
		// uptime requirement
		0x00, 0x0e, 0x7e, 0xf0,
		// secp256k1fx authorization type ID
		0x00, 0x00, 0x00, 0x0a,
		// number of signatures needed in authorization
		0x00, 0x00, 0x00, 0x01,
		// authorization signfature index
		0x00, 0x00, 0x00, 0x03,
	}
	var unsignedSimpleTransformTx UnsignedTx = simpleTransformTx
	unsignedSimpleTransformTxBytes, err := Codec.Marshal(CodecVersion, &unsignedSimpleTransformTx)
	require.NoError(err)
	require.Equal(expectedUnsignedSimpleTransformTxBytes, unsignedSimpleTransformTxBytes)

	complexTransformTx := &TransformSubnetTx{
		BaseTx: BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: constants.PlatformChainID,
				Outs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &stakeable.LockOut{
							Locktime: 87654321,
							TransferableOut: &secp256k1fx.TransferOutput{
								Amt: 1,
								OutputOwners: secp256k1fx.OutputOwners{
									Locktime:  12345678,
									Threshold: 0,
									Addrs:     []ids.ShortID{},
								},
							},
						},
					},
					{
						Asset: avax.Asset{
							ID: customAssetID,
						},
						Out: &stakeable.LockOut{
							Locktime: 876543210,
							TransferableOut: &secp256k1fx.TransferOutput{
								Amt: 0xffffffffffffffff,
								OutputOwners: secp256k1fx.OutputOwners{
									Locktime:  0,
									Threshold: 1,
									Addrs: []ids.ShortID{
										addr,
									},
								},
							},
						},
					},
				},
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
							Amt: units.KiloAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{2, 5},
							},
						},
					},
					{
						UTXOID: avax.UTXOID{
							TxID:        txID,
							OutputIndex: 2,
						},
						Asset: avax.Asset{
							ID: customAssetID,
						},
						In: &stakeable.LockIn{
							Locktime: 876543210,
							TransferableIn: &secp256k1fx.TransferInput{
								Amt: 0xefffffffffffffff,
								Input: secp256k1fx.Input{
									SigIndices: []uint32{0},
								},
							},
						},
					},
					{
						UTXOID: avax.UTXOID{
							TxID:        txID,
							OutputIndex: 3,
						},
						Asset: avax.Asset{
							ID: customAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 0x1000000000000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{},
							},
						},
					},
				},
				Memo: types.JSONByteSlice("😅\nwell that's\x01\x23\x45!"),
			},
		},
		Subnet:                   subnetID,
		AssetID:                  customAssetID,
		InitialSupply:            0x1000000000000000,
		MaximumSupply:            0x1000000000000000,
		MinConsumptionRate:       0,
		MaxConsumptionRate:       0,
		MinValidatorStake:        1,
		MaxValidatorStake:        0x1000000000000000,
		MinStakeDuration:         1,
		MaxStakeDuration:         1,
		MinDelegationFee:         0,
		MinDelegatorStake:        0xffffffffffffffff,
		MaxValidatorWeightFactor: 255,
		UptimeRequirement:        0,
		SubnetAuth: &secp256k1fx.Input{
			SigIndices: []uint32{},
		},
	}
	avax.SortTransferableOutputs(complexTransformTx.Outs, Codec)
	utils.Sort(complexTransformTx.Ins)
	require.NoError(complexTransformTx.SyntacticVerify(&snow.Context{
		NetworkID:   1,
		ChainID:     constants.PlatformChainID,
		AVAXAssetID: avaxAssetID,
	}))

	expectedUnsignedComplexTransformTxBytes := []byte{
		// Codec version
		0x00, 0x00,
		// TransformSubnetTx type ID
		0x00, 0x00, 0x00, 0x18,
		// Mainnet network ID
		0x00, 0x00, 0x00, 0x01,
		// P-chain blockchain ID
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// number of outputs
		0x00, 0x00, 0x00, 0x02,
		// outputs[0]
		// Mainnet AVAX asset ID
		0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
		0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
		0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
		0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
		// Stakeable locked output type ID
		0x00, 0x00, 0x00, 0x16,
		// Locktime
		0x00, 0x00, 0x00, 0x00, 0x05, 0x39, 0x7f, 0xb1,
		// seck256k1fx transfer output type ID
		0x00, 0x00, 0x00, 0x07,
		// amount
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		// secp256k1fx locktime
		0x00, 0x00, 0x00, 0x00, 0x00, 0xbc, 0x61, 0x4e,
		// threshold
		0x00, 0x00, 0x00, 0x00,
		// number of addresses
		0x00, 0x00, 0x00, 0x00,
		// outputs[1]
		// custom assest ID
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		// Stakeable locked output type ID
		0x00, 0x00, 0x00, 0x16,
		// Locktime
		0x00, 0x00, 0x00, 0x00, 0x34, 0x3e, 0xfc, 0xea,
		// seck256k1fx transfer output type ID
		0x00, 0x00, 0x00, 0x07,
		// amount
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		// secp256k1fx locktime
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold
		0x00, 0x00, 0x00, 0x01,
		// number of addresses
		0x00, 0x00, 0x00, 0x01,
		// address[0]
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
		// number of inputs
		0x00, 0x00, 0x00, 0x03,
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
		// amount = 1,000 AVAX
		0x00, 0x00, 0x00, 0xe8, 0xd4, 0xa5, 0x10, 0x00,
		// number of signatures indices
		0x00, 0x00, 0x00, 0x02,
		// first signature index
		0x00, 0x00, 0x00, 0x02,
		// second signature index
		0x00, 0x00, 0x00, 0x05,
		// inputs[1]
		// TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// Tx output index
		0x00, 0x00, 0x00, 0x02,
		// custom asset ID
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		// stakeable locked input type ID
		0x00, 0x00, 0x00, 0x15,
		// locktime
		0x00, 0x00, 0x00, 0x00, 0x34, 0x3e, 0xfc, 0xea,
		// secp256k1fx transfer input type ID
		0x00, 0x00, 0x00, 0x05,
		// input amount
		0xef, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		// number of signatures needed in input
		0x00, 0x00, 0x00, 0x01,
		// index of signer
		0x00, 0x00, 0x00, 0x00,
		// inputs[2]
		// TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// Tx output index
		0x00, 0x00, 0x00, 0x03,
		// custom asset ID
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		// secp256k1fx transfer input type ID
		0x00, 0x00, 0x00, 0x05,
		// input amount
		0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// number of signatures needed in input
		0x00, 0x00, 0x00, 0x00,
		// memo length
		0x00, 0x00, 0x00, 0x14,
		// memo
		0xf0, 0x9f, 0x98, 0x85, 0x0a, 0x77, 0x65, 0x6c,
		0x6c, 0x20, 0x74, 0x68, 0x61, 0x74, 0x27, 0x73,
		0x01, 0x23, 0x45, 0x21,
		// subnetID being transformed
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
		// staking asset ID
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		// initial supply
		0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// maximum supply
		0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// minimum consumption rate
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// maximum consumption rate
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// minimum staking amount
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		// maximum staking amount
		0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// minimum staking duration
		0x00, 0x00, 0x00, 0x01,
		// maximum staking duration
		0x00, 0x00, 0x00, 0x01,
		// minimum delegation fee
		0x00, 0x00, 0x00, 0x00,
		// minimum delegation amount
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		// maximum validator weight factor
		0xff,
		// uptime requirement
		0x00, 0x00, 0x00, 0x00,
		// secp256k1fx authorization type ID
		0x00, 0x00, 0x00, 0x0a,
		// number of signatures needed in authorization
		0x00, 0x00, 0x00, 0x00,
	}
	var unsignedComplexTransformTx UnsignedTx = complexTransformTx
	unsignedComplexTransformTxBytes, err := Codec.Marshal(CodecVersion, &unsignedComplexTransformTx)
	require.NoError(err)
	require.Equal(expectedUnsignedComplexTransformTxBytes, unsignedComplexTransformTxBytes)

	aliaser := ids.NewAliaser()
	require.NoError(aliaser.Alias(constants.PlatformChainID, "P"))

	unsignedComplexTransformTx.InitCtx(&snow.Context{
		NetworkID:   1,
		ChainID:     constants.PlatformChainID,
		AVAXAssetID: avaxAssetID,
		BCLookup:    aliaser,
	})

	unsignedComplexTransformTxJSONBytes, err := json.MarshalIndent(unsignedComplexTransformTx, "", "\t")
	require.NoError(err)
	require.JSONEq(`{
	"networkID": 1,
	"blockchainID": "11111111111111111111111111111111LpoYY",
	"outputs": [
		{
			"assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"output": {
				"locktime": 87654321,
				"output": {
					"addresses": [],
					"amount": 1,
					"locktime": 12345678,
					"threshold": 0
				}
			}
		},
		{
			"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"output": {
				"locktime": 876543210,
				"output": {
					"addresses": [
						"P-avax1g32kvaugnx4tk3z4vemc3xd2hdz92enh972wxr"
					],
					"amount": 18446744073709551615,
					"locktime": 0,
					"threshold": 1
				}
			}
		}
	],
	"inputs": [
		{
			"txID": "2wiU5PnFTjTmoAXGZutHAsPF36qGGyLHYHj9G1Aucfmb3JFFGN",
			"outputIndex": 1,
			"assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"input": {
				"amount": 1000000000000,
				"signatureIndices": [
					2,
					5
				]
			}
		},
		{
			"txID": "2wiU5PnFTjTmoAXGZutHAsPF36qGGyLHYHj9G1Aucfmb3JFFGN",
			"outputIndex": 2,
			"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"input": {
				"locktime": 876543210,
				"input": {
					"amount": 17293822569102704639,
					"signatureIndices": [
						0
					]
				}
			}
		},
		{
			"txID": "2wiU5PnFTjTmoAXGZutHAsPF36qGGyLHYHj9G1Aucfmb3JFFGN",
			"outputIndex": 3,
			"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"input": {
				"amount": 1152921504606846976,
				"signatureIndices": []
			}
		}
	],
	"memo": "0xf09f98850a77656c6c2074686174277301234521",
	"subnetID": "SkB92YpWm4UpburLz9tEKZw2i67H3FF6YkjaU4BkFUDTG9Xm",
	"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
	"initialSupply": 1152921504606846976,
	"maximumSupply": 1152921504606846976,
	"minConsumptionRate": 0,
	"maxConsumptionRate": 0,
	"minValidatorStake": 1,
	"maxValidatorStake": 1152921504606846976,
	"minStakeDuration": 1,
	"maxStakeDuration": 1,
	"minDelegationFee": 0,
	"minDelegatorStake": 18446744073709551615,
	"maxValidatorWeightFactor": 255,
	"uptimeRequirement": 0,
	"subnetAuthorization": {
		"signatureIndices": []
	}
}`, string(unsignedComplexTransformTxJSONBytes))
}

func TestTransformSubnetTxSyntacticVerify(t *testing.T) {
	type test struct {
		name   string
		txFunc func(*gomock.Controller) *TransformSubnetTx
		err    error
	}

	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	ctx := &snow.Context{
		ChainID:     chainID,
		NetworkID:   networkID,
		AVAXAssetID: ids.GenerateTestID(),
	}

	// A BaseTx that already passed syntactic verification.
	verifiedBaseTx := BaseTx{
		SyntacticallyVerified: true,
	}

	// A BaseTx that passes syntactic verification.
	validBaseTx := BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx: verifiedBaseTx,
				}
			},
			err: nil,
		},
		{
			name: "invalid subnetID",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx: validBaseTx,
					Subnet: constants.PrimaryNetworkID,
				}
			},
			err: errCantTransformPrimaryNetwork,
		},
		{
			name: "empty assetID",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:  validBaseTx,
					Subnet:  ids.GenerateTestID(),
					AssetID: ids.Empty,
				}
			},
			err: errEmptyAssetID,
		},
		{
			name: "AVAX assetID",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:  validBaseTx,
					Subnet:  ids.GenerateTestID(),
					AssetID: ctx.AVAXAssetID,
				}
			},
			err: errAssetIDCantBeAVAX,
		},
		{
			name: "initialSupply == 0",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:        validBaseTx,
					Subnet:        ids.GenerateTestID(),
					AssetID:       ids.GenerateTestID(),
					InitialSupply: 0,
				}
			},
			err: errInitialSupplyZero,
		},
		{
			name: "initialSupply > maximumSupply",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:        validBaseTx,
					Subnet:        ids.GenerateTestID(),
					AssetID:       ids.GenerateTestID(),
					InitialSupply: 2,
					MaximumSupply: 1,
				}
			},
			err: errInitialSupplyGreaterThanMaxSupply,
		},
		{
			name: "minConsumptionRate > maxConsumptionRate",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 2,
					MaxConsumptionRate: 1,
				}
			},
			err: errMinConsumptionRateTooLarge,
		},
		{
			name: "maxConsumptionRate > 100%",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator + 1,
				}
			},
			err: errMaxConsumptionRateTooLarge,
		},
		{
			name: "minValidatorStake == 0",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  0,
				}
			},
			err: errMinValidatorStakeZero,
		},
		{
			name: "minValidatorStake > initialSupply",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
				}
			},
			err: errMinValidatorStakeAboveSupply,
		},
		{
			name: "minValidatorStake > maxValidatorStake",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  1,
				}
			},
			err: errMinValidatorStakeAboveMax,
		},
		{
			name: "maxValidatorStake > maximumSupply",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  11,
				}
			},
			err: errMaxValidatorStakeTooLarge,
		},
		{
			name: "minStakeDuration == 0",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   0,
				}
			},
			err: errMinStakeDurationZero,
		},
		{
			name: "minStakeDuration > maxStakeDuration",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   2,
					MaxStakeDuration:   1,
				}
			},
			err: errMinStakeDurationTooLarge,
		},
		{
			name: "minDelegationFee > 100%",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   1,
					MaxStakeDuration:   2,
					MinDelegationFee:   reward.PercentDenominator + 1,
				}
			},
			err: errMinDelegationFeeTooLarge,
		},
		{
			name: "minDelegatorStake == 0",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:             validBaseTx,
					Subnet:             ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   1,
					MaxStakeDuration:   2,
					MinDelegationFee:   reward.PercentDenominator,
					MinDelegatorStake:  0,
				}
			},
			err: errMinDelegatorStakeZero,
		},
		{
			name: "maxValidatorWeightFactor == 0",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:                   validBaseTx,
					Subnet:                   ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 0,
				}
			},
			err: errMaxValidatorWeightFactorZero,
		},
		{
			name: "uptimeRequirement > 100%",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:                   validBaseTx,
					Subnet:                   ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator + 1,
				}
			},
			err: errUptimeRequirementTooLarge,
		},
		{
			name: "invalid subnetAuth",
			txFunc: func(ctrl *gomock.Controller) *TransformSubnetTx {
				// This SubnetAuth fails verification.
				invalidSubnetAuth := verifymock.NewVerifiable(ctrl)
				invalidSubnetAuth.EXPECT().Verify().Return(errInvalidSubnetAuth)
				return &TransformSubnetTx{
					BaseTx:                   validBaseTx,
					Subnet:                   ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator,
					SubnetAuth:               invalidSubnetAuth,
				}
			},
			err: errInvalidSubnetAuth,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(*gomock.Controller) *TransformSubnetTx {
				return &TransformSubnetTx{
					BaseTx:                   invalidBaseTx,
					Subnet:                   ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator,
				}
			},
			err: avax.ErrWrongNetworkID,
		},
		{
			name: "passes verification",
			txFunc: func(ctrl *gomock.Controller) *TransformSubnetTx {
				// This SubnetAuth passes verification.
				validSubnetAuth := verifymock.NewVerifiable(ctrl)
				validSubnetAuth.EXPECT().Verify().Return(nil)
				return &TransformSubnetTx{
					BaseTx:                   validBaseTx,
					Subnet:                   ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator,
					SubnetAuth:               validSubnetAuth,
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(ctx)
			require.ErrorIs(t, err, tt.err)
		})
	}
}
