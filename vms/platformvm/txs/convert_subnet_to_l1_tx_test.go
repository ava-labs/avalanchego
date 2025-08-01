// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	_ "embed"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var (
	//go:embed convert_subnet_to_l1_tx_test_simple.json
	convertSubnetToL1TxSimpleJSON []byte
	//go:embed convert_subnet_to_l1_tx_test_complex.json
	convertSubnetToL1TxComplexJSON []byte
)

func TestConvertSubnetToL1TxSerialization(t *testing.T) {
	skBytes, err := hex.DecodeString("6668fecd4595b81e4d568398c820bbf3f073cb222902279fa55ebb84764ed2e3")
	require.NoError(t, err)
	sk, err := localsigner.FromBytes(skBytes)
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	var (
		addr = ids.ShortID{
			0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
			0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
			0x44, 0x55, 0x66, 0x77,
		}
		avaxAssetID = ids.ID{
			0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
			0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
			0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
			0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
		}
		customAssetID = ids.ID{
			0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
			0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
			0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
			0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		}
		txID = ids.ID{
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		}
		subnetID = ids.ID{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
			0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
			0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
		}
		managerChainID = ids.ID{
			0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
			0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		}
		managerAddress = []byte{
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0xde, 0xad,
		}
		nodeID = ids.BuildTestNodeID([]byte{
			0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
			0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
			0x11, 0x22, 0x33, 0x44,
		})
	)

	tests := []struct {
		name          string
		tx            *ConvertSubnetToL1Tx
		expectedBytes []byte
		expectedJSON  []byte
	}{
		{
			name: "simple",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    constants.UnitTestID,
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
									Amt: units.MilliAvax,
									Input: secp256k1fx.Input{
										SigIndices: []uint32{5},
									},
								},
							},
						},
						Memo: types.JSONByteSlice{},
					},
				},
				Subnet:     subnetID,
				ChainID:    managerChainID,
				Address:    managerAddress,
				Validators: []*ConvertSubnetToL1Validator{},
				SubnetAuth: &secp256k1fx.Input{
					SigIndices: []uint32{3},
				},
			},
			expectedBytes: []byte{
				// Codec version
				0x00, 0x00,
				// ConvertSubnetToL1Tx Type ID
				0x00, 0x00, 0x00, 0x23,
				// Network ID
				0x00, 0x00, 0x00, 0x0a,
				// P-chain blockchain ID
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Number of outputs
				0x00, 0x00, 0x00, 0x00,
				// Number of inputs
				0x00, 0x00, 0x00, 0x01,
				// Inputs[0]
				// TxID
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				// Tx output index
				0x00, 0x00, 0x00, 0x01,
				// AVAX assetID
				0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
				0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
				0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
				0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
				// secp256k1fx transfer input type ID
				0x00, 0x00, 0x00, 0x05,
				// input amount = 1 MilliAvax
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0f, 0x42, 0x40,
				// number of signatures needed in input
				0x00, 0x00, 0x00, 0x01,
				// index of signer
				0x00, 0x00, 0x00, 0x05,
				// length of memo
				0x00, 0x00, 0x00, 0x00,
				// subnetID to modify
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
				// chainID of the manager
				0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				// length of the manager address
				0x00, 0x00, 0x00, 0x14,
				// address of the manager
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0xde, 0xad,
				// number of validators
				0x00, 0x00, 0x00, 0x00,
				// secp256k1fx authorization type ID
				0x00, 0x00, 0x00, 0x0a,
				// number of signatures needed in authorization
				0x00, 0x00, 0x00, 0x01,
				// index of signer
				0x00, 0x00, 0x00, 0x03,
			},
			expectedJSON: convertSubnetToL1TxSimpleJSON,
		},
		{
			name: "complex",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    constants.UnitTestID,
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
									Amt: units.Avax,
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
				Subnet:  subnetID,
				ChainID: managerChainID,
				Address: managerAddress,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:  nodeID[:],
						Weight:  0x0102030405060708,
						Balance: units.Avax,
						Signer:  *pop,
						RemainingBalanceOwner: message.PChainOwner{
							Threshold: 1,
							Addresses: []ids.ShortID{
								addr,
							},
						},
						DeactivationOwner: message.PChainOwner{
							Threshold: 1,
							Addresses: []ids.ShortID{
								addr,
							},
						},
					},
				},
				SubnetAuth: &secp256k1fx.Input{
					SigIndices: []uint32{},
				},
			},
			expectedBytes: []byte{
				// Codec version
				0x00, 0x00,
				// ConvertSubnetToL1Tx Type ID
				0x00, 0x00, 0x00, 0x23,
				// Network ID
				0x00, 0x00, 0x00, 0x0a,
				// P-chain blockchain ID
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Number of outputs
				0x00, 0x00, 0x00, 0x02,
				// Outputs[0]
				// AVAX assetID
				0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
				0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
				0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
				0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
				// Stakeable locked output type ID
				0x00, 0x00, 0x00, 0x16,
				// Locktime
				0x00, 0x00, 0x00, 0x00, 0x05, 0x39, 0x7f, 0xb1,
				// secp256k1fx transfer output type ID
				0x00, 0x00, 0x00, 0x07,
				// amount
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				// secp256k1fx output locktime
				0x00, 0x00, 0x00, 0x00, 0x00, 0xbc, 0x61, 0x4e,
				// threshold
				0x00, 0x00, 0x00, 0x00,
				// number of addresses
				0x00, 0x00, 0x00, 0x00,
				// Outputs[1]
				// custom asset ID
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				// Stakeable locked output type ID
				0x00, 0x00, 0x00, 0x16,
				// Locktime
				0x00, 0x00, 0x00, 0x00, 0x34, 0x3e, 0xfc, 0xea,
				// secp256k1fx transfer output type ID
				0x00, 0x00, 0x00, 0x07,
				// amount
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
				// secp256k1fx output locktime
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
				// AVAX assetID
				0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
				0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
				0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
				0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
				// secp256k1fx transfer input type ID
				0x00, 0x00, 0x00, 0x05,
				// input amount = 1 Avax
				0x00, 0x00, 0x00, 0x00, 0x3b, 0x9a, 0xca, 0x00,
				// number of signatures needed in input
				0x00, 0x00, 0x00, 0x02,
				// index of first signer
				0x00, 0x00, 0x00, 0x02,
				// index of second signer
				0x00, 0x00, 0x00, 0x05,
				// inputs[1]
				// TxID
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
				// Tx output index
				0x00, 0x00, 0x00, 0x02,
				// Custom asset ID
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
				// Stakeable locked input type ID
				0x00, 0x00, 0x00, 0x15,
				// Locktime
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
				// length of memo
				0x00, 0x00, 0x00, 0x14,
				// memo
				0xf0, 0x9f, 0x98, 0x85, 0x0a, 0x77, 0x65, 0x6c,
				0x6c, 0x20, 0x74, 0x68, 0x61, 0x74, 0x27, 0x73,
				0x01, 0x23, 0x45, 0x21,
				// subnetID to modify
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
				// chainID of the manager
				0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				// length of the manager address
				0x00, 0x00, 0x00, 0x14,
				// address of the manager
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0xde, 0xad,
				// number of validators
				0x00, 0x00, 0x00, 0x01,
				// Validators[0]
				// node ID length
				0x00, 0x00, 0x00, 0x14,
				// node ID
				0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
				0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
				0x11, 0x22, 0x33, 0x44,
				// weight
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				// balance
				0x00, 0x00, 0x00, 0x00, 0x3b, 0x9a, 0xca, 0x00,
				// BLS compressed public key
				0xaf, 0xf4, 0xac, 0xb4, 0xc5, 0x43, 0x9b, 0x5d,
				0x42, 0x6c, 0xad, 0xf9, 0xe9, 0x46, 0xd3, 0xa4,
				0x52, 0xf7, 0xde, 0x34, 0x14, 0xd1, 0xad, 0x27,
				0x33, 0x61, 0x33, 0x21, 0x1d, 0x8b, 0x90, 0xcf,
				0x49, 0xfb, 0x97, 0xee, 0xbc, 0xde, 0xee, 0xf7,
				0x14, 0xdc, 0x20, 0xf5, 0x4e, 0xd0, 0xd4, 0xd1,
				// BLS compressed signature
				0x8c, 0xfd, 0x79, 0x09, 0xd1, 0x53, 0xb9, 0x60,
				0x4b, 0x62, 0xb1, 0x43, 0xba, 0x36, 0x20, 0x7b,
				0xb7, 0xe6, 0x48, 0x67, 0x42, 0x44, 0x80, 0x20,
				0x2a, 0x67, 0xdc, 0x68, 0x76, 0x83, 0x46, 0xd9,
				0x5c, 0x90, 0x98, 0x3c, 0x2d, 0x27, 0x9c, 0x64,
				0xc4, 0x3c, 0x51, 0x13, 0x6b, 0x2a, 0x05, 0xe0,
				0x16, 0x02, 0xd5, 0x2a, 0xa6, 0x37, 0x6f, 0xda,
				0x17, 0xfa, 0x6e, 0x2a, 0x18, 0xa0, 0x83, 0xe4,
				0x9d, 0x9c, 0x45, 0x0e, 0xab, 0x7b, 0x89, 0xb1,
				0xd5, 0x55, 0x5d, 0xa5, 0xc4, 0x89, 0x87, 0x2e,
				0x02, 0xb7, 0xe5, 0x22, 0x7b, 0x77, 0x55, 0x0a,
				0xf1, 0x33, 0x0e, 0x5a, 0x71, 0xf8, 0xc3, 0x68,
				// RemainingBalanceOwner threshold
				0x00, 0x00, 0x00, 0x01,
				// RemainingBalanceOwner number of addresses
				0x00, 0x00, 0x00, 0x01,
				// RemainingBalanceOwner Addrs[0]
				0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
				0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
				0x44, 0x55, 0x66, 0x77,
				// DeactivationOwner threshold
				0x00, 0x00, 0x00, 0x01,
				// DeactivationOwner number of addresses
				0x00, 0x00, 0x00, 0x01,
				// DeactivationOwner Addrs[0]
				0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
				0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
				0x44, 0x55, 0x66, 0x77,
				// secp256k1fx authorization type ID
				0x00, 0x00, 0x00, 0x0a,
				// number of signatures needed in authorization
				0x00, 0x00, 0x00, 0x00,
			},
			expectedJSON: convertSubnetToL1TxComplexJSON,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			var unsignedTx UnsignedTx = test.tx
			txBytes, err := Codec.Marshal(CodecVersion, &unsignedTx)
			require.NoError(err)
			require.Equal(test.expectedBytes, txBytes)

			ctx := snowtest.Context(t, constants.PlatformChainID)
			test.tx.InitCtx(ctx)

			txJSON, err := json.MarshalIndent(test.tx, "", "\t")
			require.NoError(err)
			require.JSONEq(string(test.expectedJSON), string(txJSON))
		})
	}
}

func TestConvertSubnetToL1TxSyntacticVerify(t *testing.T) {
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	var (
		ctx         = snowtest.Context(t, ids.GenerateTestID())
		validBaseTx = BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
			},
		}
		validSubnetID   = ids.GenerateTestID()
		invalidAddress  = make(types.JSONByteSlice, MaxSubnetAddressLength+1)
		validValidators = []*ConvertSubnetToL1Validator{
			{
				NodeID:                utils.RandomBytes(ids.NodeIDLen),
				Weight:                1,
				Balance:               1,
				Signer:                *pop,
				RemainingBalanceOwner: message.PChainOwner{},
				DeactivationOwner:     message.PChainOwner{},
			},
		}
		validSubnetAuth   = &secp256k1fx.Input{}
		invalidSubnetAuth = &secp256k1fx.Input{
			SigIndices: []uint32{1, 0},
		}
	)

	tests := []struct {
		name        string
		tx          *ConvertSubnetToL1Tx
		expectedErr error
	}{
		{
			name:        "nil tx",
			tx:          nil,
			expectedErr: ErrNilTx,
		},
		{
			name: "already verified",
			// The tx includes invalid data to verify that a cached result is
			// returned.
			tx: &ConvertSubnetToL1Tx{
				BaseTx: BaseTx{
					SyntacticallyVerified: true,
				},
				Subnet:     constants.PrimaryNetworkID,
				Address:    invalidAddress,
				Validators: nil,
				SubnetAuth: invalidSubnetAuth,
			},
			expectedErr: nil,
		},
		{
			name: "invalid subnetID",
			tx: &ConvertSubnetToL1Tx{
				BaseTx:     validBaseTx,
				Subnet:     constants.PrimaryNetworkID,
				Validators: validValidators,
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: ErrConvertPermissionlessSubnet,
		},
		{
			name: "invalid address",
			tx: &ConvertSubnetToL1Tx{
				BaseTx:     validBaseTx,
				Subnet:     validSubnetID,
				Address:    invalidAddress,
				Validators: validValidators,
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: ErrAddressTooLong,
		},
		{
			name: "invalid number of validators",
			tx: &ConvertSubnetToL1Tx{
				BaseTx:     validBaseTx,
				Subnet:     validSubnetID,
				Validators: nil,
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: ErrConvertMustIncludeValidators,
		},
		{
			name: "invalid validator order",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: validBaseTx,
				Subnet: validSubnetID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID: []byte{
							0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00,
						},
					},
					{
						NodeID: []byte{
							0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
							0x00, 0x00, 0x00, 0x00,
						},
					},
				},
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: ErrConvertValidatorsNotSortedAndUnique,
		},
		{
			name: "invalid validator weight",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: validBaseTx,
				Subnet: validSubnetID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen),
						Weight:                0,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: ErrZeroWeight,
		},
		{
			name: "invalid validator nodeID length",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: validBaseTx,
				Subnet: validSubnetID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen + 1),
						Weight:                1,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: hashing.ErrInvalidHashLen,
		},
		{
			name: "invalid validator nodeID",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: validBaseTx,
				Subnet: validSubnetID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                ids.EmptyNodeID[:],
						Weight:                1,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: errEmptyNodeID,
		},
		{
			name: "invalid validator pop",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: validBaseTx,
				Subnet: validSubnetID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen),
						Weight:                1,
						Signer:                signer.ProofOfPossession{},
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					},
				},
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: bls.ErrFailedPublicKeyDecompress,
		},
		{
			name: "invalid validator remaining balance owner",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: validBaseTx,
				Subnet: validSubnetID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID: utils.RandomBytes(ids.NodeIDLen),
						Weight: 1,
						Signer: *pop,
						RemainingBalanceOwner: message.PChainOwner{
							Threshold: 1,
						},
						DeactivationOwner: message.PChainOwner{},
					},
				},
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "invalid validator deactivation owner",
			tx: &ConvertSubnetToL1Tx{
				BaseTx: validBaseTx,
				Subnet: validSubnetID,
				Validators: []*ConvertSubnetToL1Validator{
					{
						NodeID:                utils.RandomBytes(ids.NodeIDLen),
						Weight:                1,
						Signer:                *pop,
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner: message.PChainOwner{
							Threshold: 1,
						},
					},
				},
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "invalid BaseTx",
			tx: &ConvertSubnetToL1Tx{
				BaseTx:     BaseTx{},
				Subnet:     validSubnetID,
				Validators: validValidators,
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid subnetAuth",
			tx: &ConvertSubnetToL1Tx{
				BaseTx:     validBaseTx,
				Subnet:     validSubnetID,
				Validators: validValidators,
				SubnetAuth: invalidSubnetAuth,
			},
			expectedErr: secp256k1fx.ErrInputIndicesNotSortedUnique,
		},
		{
			name: "passes verification",
			tx: &ConvertSubnetToL1Tx{
				BaseTx:     validBaseTx,
				Subnet:     validSubnetID,
				Validators: validValidators,
				SubnetAuth: validSubnetAuth,
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.tx.SyntacticVerify(ctx)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			require.True(test.tx.SyntacticallyVerified)
		})
	}
}
