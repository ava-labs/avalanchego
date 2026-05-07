// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx_test

import (
	"context"
	"math"
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Imported for [vm.VerifierBackend] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras/extrastest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
	safemath "github.com/ava-labs/avalanchego/utils/math"

	. "github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	os.Exit(m.Run())
}

type golden struct {
	name  string
	old   *atomic.Tx
	new   *Tx
	id    ids.ID
	bytes []byte
}

// These are the mainnet values.
var (
	avaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
	cChainID    = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	xChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
)

// Golden transactions are defined at the package level to allow sharing between
// various fuzz tests and unit tests.
var (
	importGolden = golden{
		name: "import", // Included in https://subnets.avax.network/c-chain/block/4
		old: &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedImportTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: cChainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        ids.FromStringOrPanic("2VqSFA5hxukiv1FSAB8ShjwHwmPev9ZS8VD9aUTCDRoff7T5Bi"),
						OutputIndex: 1,
					},
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					In: &secp256k1fx.TransferInput{
						Amt: 50000000,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{0},
						},
					},
				}},
				Outs: []atomic.EVMOutput{{
					Address: common.HexToAddress("0xb8b5a87d1c05676f1f966da49151fa54dbe68c33"),
					Amount:  50000000,
					AssetID: avaxAssetID,
				}},
			},
			Creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x3e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01")),
					},
				},
			},
		},
		new: &Tx{
			Unsigned: &Import{
				NetworkID:    constants.MainnetID,
				BlockchainID: cChainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        ids.FromStringOrPanic("2VqSFA5hxukiv1FSAB8ShjwHwmPev9ZS8VD9aUTCDRoff7T5Bi"),
						OutputIndex: 1,
					},
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					In: &secp256k1fx.TransferInput{
						Amt: 50000000,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{0},
						},
					},
				}},
				Outs: []Output{{
					Address: common.HexToAddress("0xb8b5a87d1c05676f1f966da49151fa54dbe68c33"),
					Amount:  50000000,
					AssetID: avaxAssetID,
				}},
			},
			Creds: []Credential{
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x3e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01")),
					},
				},
			},
		},
		id:    ids.FromStringOrPanic("h34BPNmYApCbW8buVWAtzu1KtjTFmyMhiRQQnAqPqwCqQsB7f"),
		bytes: common.FromHex("0x000000000000000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b00000001c52b712aa7dce27a650bf509f799673e245edd4fa9e4e1700eb6105202fe579a0000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000002faf080000000010000000000000001b8b5a87d1c05676f1f966da49151fa54dbe68c330000000002faf08021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000100000009000000013e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01"),
	}

	exportGolden = golden{
		name: "export", // Included in https://subnets.avax.network/c-chain/block/48
		old: &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedExportTx{
				NetworkID:        constants.MainnetID,
				BlockchainID:     cChainID,
				DestinationChain: xChainID,
				Ins: []atomic.EVMInput{{
					Address: common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"),
					Amount:  1000001,
					AssetID: avaxAssetID,
				}},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs: []ids.ShortID{
								ids.ShortFromStringOrPanic("LanVZgBDVvtarbTXD1uU7r1nXVJyLmPUz"),
							},
						},
					},
				}},
			},
			Creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01")),
					},
				},
			},
		},
		new: &Tx{
			Unsigned: &Export{
				NetworkID:        constants.MainnetID,
				BlockchainID:     cChainID,
				DestinationChain: xChainID,
				Ins: []Input{{
					Address: common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"),
					Amount:  1000001,
					AssetID: avaxAssetID,
				}},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs: []ids.ShortID{
								ids.ShortFromStringOrPanic("LanVZgBDVvtarbTXD1uU7r1nXVJyLmPUz"),
							},
						},
					},
				}},
			},
			Creds: []Credential{
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01")),
					},
				},
			},
		},
		id:    ids.FromStringOrPanic("ng7Dox1r8nctrF6zurhRPYWxkmE2juUhT7Qhpauyo8qSEu6jB"),
		bytes: common.FromHex("0x000000000001000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b00000001eb019ccd325ad53543a7e7e3b04828bdecf3cff600000000000f424121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000000000000000000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000000000000000100000000000000000000000100000001d6ce17826dd7c12a7577af257e82d99143b72500000000010000000900000001254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01"),
	}

	importMultiInputGolden = golden{
		name: "import_multi_input", // Included in https://subnets.avax.network/c-chain/block/132481
		old: &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedImportTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: cChainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.FromStringOrPanic("DqRKjysHeiKWetgyqqM2WdnX56yg8wBdY95RhuP3eDbbVoMCH"),
						},
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 99000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
					{
						UTXOID: avax.UTXOID{
							TxID: ids.FromStringOrPanic("25YuXY1zoYY3DgLsRbGjdNSx3jYtvqZRgFo6jpy7EMCfUn4S74"),
						},
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 399000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
					{
						UTXOID: avax.UTXOID{
							TxID: ids.FromStringOrPanic("2DXSj1kzqWM5HWS2PXcDSD3GUNpEGinynV1qD6LxiECHmZC8fj"),
						},
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 99000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
				Outs: []atomic.EVMOutput{
					{
						Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
						Amount:  99000000,
						AssetID: avaxAssetID,
					},
					{
						Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
						Amount:  99000000,
						AssetID: avaxAssetID,
					},
					{
						Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
						Amount:  399000000,
						AssetID: avaxAssetID,
					},
				},
			},
			Creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
					},
				},
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
					},
				},
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
					},
				},
			},
		},
		new: &Tx{
			Unsigned: &Import{
				NetworkID:    constants.MainnetID,
				BlockchainID: cChainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.FromStringOrPanic("DqRKjysHeiKWetgyqqM2WdnX56yg8wBdY95RhuP3eDbbVoMCH"),
						},
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 99000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
					{
						UTXOID: avax.UTXOID{
							TxID: ids.FromStringOrPanic("25YuXY1zoYY3DgLsRbGjdNSx3jYtvqZRgFo6jpy7EMCfUn4S74"),
						},
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 399000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
					{
						UTXOID: avax.UTXOID{
							TxID: ids.FromStringOrPanic("2DXSj1kzqWM5HWS2PXcDSD3GUNpEGinynV1qD6LxiECHmZC8fj"),
						},
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 99000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
				Outs: []Output{
					{
						Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
						Amount:  99000000,
						AssetID: avaxAssetID,
					},
					{
						Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
						Amount:  99000000,
						AssetID: avaxAssetID,
					},
					{
						Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
						Amount:  399000000,
						AssetID: avaxAssetID,
					},
				},
			},
			Creds: []Credential{
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
					},
				},
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
					},
				},
				&secp256k1fx.Credential{
					Sigs: []txtest.Signature{
						txtest.Signature(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
					},
				},
			},
		},
		id:    ids.FromStringOrPanic("2Av7bXLRwxiQhbT9EcQd8KRM3Lz6VkpTqf3Y1AT5peHZ4YAohS"),
		bytes: common.FromHex("0x000000000000000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b000000031d249d0aab138afe01e6eff9c4789018a600771d94f5396b5df7b9d05298714d0000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000005e69ec000000001000000008e0713e47bfc29bef4cee6e4635da1c74a3aabade68ccad6fca3e99fd827eb1c0000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000017c841c00000000100000000a022a8b069a5d5e54c7e09c5c5b0f762c6751068bef15fe951a5e4b349d642200000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000005e69ec0000000010000000000000003383c293db6be7ac246f0956ad632344dc2cd1da30000000005e69ec021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff383c293db6be7ac246f0956ad632344dc2cd1da30000000005e69ec021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff383c293db6be7ac246f0956ad632344dc2cd1da30000000017c841c021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000300000009000000014e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b342570000000009000000014e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b342570000000009000000014e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700"),
	}

	exportSameAddressMultiAssetGolden = golden{
		name: "export_same_address_multi_asset", // Synthetic
		old: &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedExportTx{
				Ins: []atomic.EVMInput{
					{
						Amount: 999,
						Nonce:  5,
					},
					{
						Amount:  1_000_000,
						AssetID: avaxAssetID,
						Nonce:   5,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Out: &secp256k1fx.TransferOutput{
							Amt: 100,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{{0xaa}},
							},
						},
					},
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: 100_000,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{{0xaa}},
							},
						},
					},
				},
			},
			Creds: []verify.Verifiable{},
		},
		new: &Tx{
			Unsigned: &Export{
				Ins: []Input{
					{
						Amount: 999,
						Nonce:  5,
					},
					{
						Amount:  1_000_000,
						AssetID: avaxAssetID,
						Nonce:   5,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Out: &secp256k1fx.TransferOutput{
							Amt: 100,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{{0xaa}},
							},
						},
					},
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: 100_000,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{{0xaa}},
							},
						},
					},
				},
			},
			Creds: []Credential{},
		},
		id:    ids.FromStringOrPanic("dLYGLJkvGarYPHnfRqK8zH9nu6dj6Ajf1Wjtm8X7fxr5jvvL7"),
		bytes: common.FromHex("0x000000000001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000003e700000000000000000000000000000000000000000000000000000000000000000000000000000005000000000000000000000000000000000000000000000000000f424021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000000000000500000002000000000000000000000000000000000000000000000000000000000000000000000007000000000000006400000000000000000000000100000001aa0000000000000000000000000000000000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000700000000000186a000000000000000000000000100000001aa0000000000000000000000000000000000000000000000"),
	}

	exportMultiAddressMultiAssetGolden = golden{
		name: "export_multi_address_multi_asset", // Synthetic
		old: &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedExportTx{
				Ins: []atomic.EVMInput{
					{
						Address: common.Address{1},
						Amount:  999,
						Nonce:   5,
					},
					{
						Address: common.Address{2},
						Amount:  1_000_000,
						AssetID: avaxAssetID,
						Nonce:   7,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Out: &secp256k1fx.TransferOutput{
							Amt: 500,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 2,
								Addrs:     []ids.ShortID{{0xbb}, {0xcc}},
							},
						},
					},
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: 500_000,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 2,
								Addrs:     []ids.ShortID{{0xbb}, {0xcc}},
							},
						},
					},
				},
			},
			Creds: []verify.Verifiable{},
		},
		new: &Tx{
			Unsigned: &Export{
				Ins: []Input{
					{
						Address: common.Address{1},
						Amount:  999,
						Nonce:   5,
					},
					{
						Address: common.Address{2},
						Amount:  1_000_000,
						AssetID: avaxAssetID,
						Nonce:   7,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Out: &secp256k1fx.TransferOutput{
							Amt: 500,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 2,
								Addrs:     []ids.ShortID{{0xbb}, {0xcc}},
							},
						},
					},
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: 500_000,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 2,
								Addrs:     []ids.ShortID{{0xbb}, {0xcc}},
							},
						},
					},
				},
			},
			Creds: []Credential{},
		},
		id:    ids.FromStringOrPanic("2cfgJ1XjwjNVvF4ZoW86Sc77z7TDMyGB33edRioEfuLkyKkkob"),
		bytes: common.FromHex("0x000000000001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002010000000000000000000000000000000000000000000000000003e700000000000000000000000000000000000000000000000000000000000000000000000000000005020000000000000000000000000000000000000000000000000f424021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000000000000070000000200000000000000000000000000000000000000000000000000000000000000000000000700000000000001f400000000000000000000000200000002bb00000000000000000000000000000000000000cc0000000000000000000000000000000000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000000000007a12000000000000000000000000200000002bb00000000000000000000000000000000000000cc0000000000000000000000000000000000000000000000"),
	}

	importNonAVAXGolden = golden{
		name: "import_non_avax", // Synthetic
		old: &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedImportTx{
				ImportedInputs: []*avax.TransferableInput{{
					In: &secp256k1fx.TransferInput{
						Amt: 999,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{},
						},
					},
				}},
				Outs: []atomic.EVMOutput{{
					Amount: 999,
				}},
			},
			Creds: []verify.Verifiable{},
		},
		new: &Tx{
			Unsigned: &Import{
				ImportedInputs: []*avax.TransferableInput{{
					In: &secp256k1fx.TransferInput{
						Amt: 999,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{},
						},
					},
				}},
				Outs: []Output{{
					Amount: 999,
				}},
			},
			Creds: []Credential{},
		},
		id:    ids.FromStringOrPanic("s4xoHkf4rPQYSwjbQo78hcSP1wSeViV1Fx2PHM4AfRiDurFkf"),
		bytes: common.FromHex("0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000500000000000003e70000000000000001000000000000000000000000000000000000000000000000000003e7000000000000000000000000000000000000000000000000000000000000000000000000"),
	}
)

func TestInputIDs(t *testing.T) {
	tests := []struct {
		tx   golden
		want set.Set[ids.ID]
	}{
		{
			tx: importGolden,
			want: set.Of(
				ids.ID(common.FromHex("0xfd9e10917c4a2dab395683cfb766cdc584eba118bc22d3d0fc356fb79345cf64")),
			),
		},
		{
			tx: exportGolden,
			want: set.Of(
				ids.ID(common.FromHex("0x000000000000000000000014eb019ccd325ad53543a7e7e3b04828bdecf3cff6")),
			),
		},
		{
			tx: importMultiInputGolden,
			want: set.Of(
				ids.ID(common.FromHex("0x821514ed5d925142159bc2c78bc56b043200e53aab79e97ca75e7ca7f6a96d05")),
				ids.ID(common.FromHex("0xea05e5c7135613b689d9f6b9903f431067ed72a2957ca82a652de1e8fef2c630")),
				ids.ID(common.FromHex("0xd71fb48751f6d5732e7ff63168ed311b40bf517b36279e326878fc3f5169a656")),
			),
		},
		{
			tx: exportSameAddressMultiAssetGolden,
			want: set.Of(
				ids.ID(common.FromHex("0x0000000000000005000000140000000000000000000000000000000000000000")),
			),
		},
		{
			tx: exportMultiAddressMultiAssetGolden,
			want: set.Of(
				ids.ID(common.FromHex("0x0000000000000005000000140100000000000000000000000000000000000000")),
				ids.ID(common.FromHex("0x0000000000000007000000140200000000000000000000000000000000000000")),
			),
		},
		{
			tx: importNonAVAXGolden,
			want: set.Of(
				ids.ID(common.FromHex("0x2c34ce1df23b838c5abf2a7f6437cca3d3067ed509ff25f11df6b11b582b51eb")),
			),
		},
	}
	for _, test := range tests {
		t.Run(test.tx.name, func(t *testing.T) {
			tx := test.tx.new
			got := tx.InputIDs()
			assert.Equalf(t, test.want, got, "%T.InputIDs()", tx)
		})
	}
}

func TestAccountInputID(t *testing.T) {
	tests := []struct {
		name    string
		address common.Address
		nonce   uint64
		want    ids.ID
	}{
		{
			name: "zero_address_zero_nonce",
			want: ids.ID(common.FromHex("0x0000000000000000000000140000000000000000000000000000000000000000")),
		},
		{
			name:    "non_zero_address_nonce_one",
			address: common.HexToAddress("0x0102030405060708090a0b0c0d0e0f1011121314"),
			nonce:   1,
			want:    ids.ID(common.FromHex("0x0000000000000001000000140102030405060708090a0b0c0d0e0f1011121314")),
		},
		{
			name:    "non_zero_address_max_nonce",
			address: common.HexToAddress("0x0102030405060708090a0b0c0d0e0f1011121314"),
			nonce:   math.MaxUint64,
			want:    ids.ID(common.FromHex("0xffffffffffffffff000000140102030405060708090a0b0c0d0e0f1011121314")),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := AccountInputID(test.address, test.nonce)
			assert.Equalf(t, test.want, got, "AccountInputID(%s, %d)", test.address, test.nonce)
		})
	}
}

func TestAsOp(t *testing.T) {
	tests := []struct {
		tx   golden
		want hook.Op
	}{
		{
			tx: importGolden,
			want: hook.Op{
				ID:  importGolden.id,
				Gas: 11230,
				Mint: map[common.Address]uint256.Int{
					common.HexToAddress("0xb8b5a87d1c05676f1f966da49151fa54dbe68c33"): ScaleAVAX(50_000_000),
				},
			},
		},
		{
			tx: exportGolden,
			want: hook.Op{
				ID:        exportGolden.id,
				Gas:       11230,
				GasFeeCap: *uint256.NewInt(1_000_000 * X2CRate / 11230),
				Burn: map[common.Address]hook.AccountDebit{
					common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"): {
						Amount:     ScaleAVAX(1_000_001),
						MinBalance: ScaleAVAX(1_000_001),
					},
				},
			},
		},
		{
			tx: importMultiInputGolden,
			want: hook.Op{
				ID:  importMultiInputGolden.id,
				Gas: 13526,
				Mint: map[common.Address]uint256.Int{
					common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"): ScaleAVAX(597_000_000),
				},
			},
		},
		{
			tx: exportSameAddressMultiAssetGolden,
			want: hook.Op{
				ID:        exportSameAddressMultiAssetGolden.id,
				Gas:       12378,
				GasFeeCap: *uint256.NewInt(900_000 * X2CRate / 12378),
				Burn: map[common.Address]hook.AccountDebit{
					{}: {
						Nonce:      5,
						Amount:     ScaleAVAX(1_000_000),
						MinBalance: ScaleAVAX(1_000_000),
					},
				},
			},
		},
		{
			tx: exportMultiAddressMultiAssetGolden,
			want: hook.Op{
				ID:        exportMultiAddressMultiAssetGolden.id,
				Gas:       12418,
				GasFeeCap: *uint256.NewInt(500_000 * X2CRate / 12418),
				Burn: map[common.Address]hook.AccountDebit{
					{1}: {
						Nonce: 5,
					},
					{2}: {
						Nonce:      7,
						Amount:     ScaleAVAX(1_000_000),
						MinBalance: ScaleAVAX(1_000_000),
					},
				},
			},
		},
		{
			tx: importNonAVAXGolden,
			want: hook.Op{
				ID:   importNonAVAXGolden.id,
				Gas:  10226,
				Mint: map[common.Address]uint256.Int{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.tx.name, func(t *testing.T) {
			tx := test.tx.new
			got, err := tx.AsOp(avaxAssetID)
			require.NoErrorf(t, err, "%T.AsOp(AVAXAssetID)", tx)
			assert.Equalf(t, test.want, got, "%T.AsOp(AVAXAssetID)", tx)
		})
	}
}

func TestAsOp_Errors(t *testing.T) {
	tests := []struct {
		name string
		tx   Unsigned
		want error
	}{
		{
			name: "export_multiple_nonces",
			tx: &Export{
				Ins: []Input{
					{
						Nonce: 0,
					},
					{
						Nonce: 1,
					},
				},
			},
			want: ErrMultipleNonces,
		},
		{
			name: "import_burned_overflow",
			tx: &Import{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: math.MaxUint64,
						},
					},
					{
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 2,
						},
					},
				},
				Outs: []Output{{
					AssetID: avaxAssetID,
					Amount:  1,
				}},
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "import_burned_intermediate_overflow",
			tx: &Import{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: math.MaxUint64,
						},
					},
					{
						Asset: avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 1,
						},
					},
				},
				Outs: []Output{{
					AssetID: avaxAssetID,
					Amount:  1,
				}},
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "import_burned_underflow",
			tx: &Import{
				ImportedInputs: []*avax.TransferableInput{{
					Asset: avax.Asset{ID: avaxAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				}},
				Outs: []Output{{
					AssetID: avaxAssetID,
					Amount:  2,
				}},
			},
			want: safemath.ErrUnderflow,
		},
		{
			name: "export_burned_overflow",
			tx: &Export{
				Ins: []Input{
					{
						Address: common.Address{0},
						AssetID: avaxAssetID,
						Amount:  math.MaxUint64,
					},
					{
						Address: common.Address{1},
						AssetID: avaxAssetID,
						Amount:  2,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				}},
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "export_burned_intermediate_overflow",
			tx: &Export{
				Ins: []Input{
					{
						Address: common.Address{0},
						AssetID: avaxAssetID,
						Amount:  math.MaxUint64,
					},
					{
						Address: common.Address{1},
						AssetID: avaxAssetID,
						Amount:  1,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				}},
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "export_burned_underflow",
			tx: &Export{
				Ins: []Input{{
					AssetID: avaxAssetID,
					Amount:  1,
				}},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 2,
					},
				}},
			},
			want: safemath.ErrUnderflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &Tx{
				Unsigned: test.tx,
			}
			_, err := tx.AsOp(avaxAssetID)
			require.ErrorIsf(t, err, test.want, "%T.AsOp(AVAXAssetID)", tx)
		})
	}
}

// asOpStateDB is an in-memory [atomic.StateDB] for [FuzzAsOpCompatibility]. It
// constructs a [hook.Op] from [atomic.UnsignedAtomicTx.EVMStateTransfer].
type asOpStateDB struct {
	initialNonces map[common.Address]uint64
	op            hook.Op
}

func newAsOpStateDB() *asOpStateDB {
	return &asOpStateDB{
		initialNonces: make(map[common.Address]uint64),
		op: hook.Op{
			Burn: make(map[common.Address]hook.AccountDebit),
			Mint: make(map[common.Address]uint256.Int),
		},
	}
}

func (s *asOpStateDB) SetNonce(addr common.Address, nonce uint64) {
	d := s.op.Burn[addr]
	// The op specifies what nonce is being consumed, not the next nonce. So we
	// need to subtract 1.
	d.Nonce = nonce - 1
	s.op.Burn[addr] = d
}

func (s *asOpStateDB) SubBalance(addr common.Address, amount *uint256.Int) {
	d := s.op.Burn[addr]
	d.Amount.Add(&d.Amount, amount)
	d.MinBalance = d.Amount
	s.op.Burn[addr] = d
}

func (s *asOpStateDB) AddBalance(addr common.Address, amount *uint256.Int) {
	b := s.op.Mint[addr]
	b.Add(&b, amount)
	s.op.Mint[addr] = b
}

// largeUint256 and largeBigInt return a balance large enough to never underflow
// but small enough to never overflow during test arithmetic.
func largeUint256() *uint256.Int { return new(uint256.Int).Lsh(uint256.NewInt(1), 128) }
func largeBigInt() *big.Int      { return new(big.Int).Lsh(big.NewInt(1), 128) }

func (s *asOpStateDB) GetNonce(addr common.Address) uint64                     { return s.initialNonces[addr] }
func (*asOpStateDB) GetBalance(common.Address) *uint256.Int                    { return largeUint256() }
func (*asOpStateDB) AddBalanceMultiCoin(common.Address, common.Hash, *big.Int) {}
func (*asOpStateDB) SubBalanceMultiCoin(common.Address, common.Hash, *big.Int) {}
func (*asOpStateDB) GetBalanceMultiCoin(common.Address, common.Hash) *big.Int  { return largeBigInt() }

func FuzzAsOpCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		got, err := newTx.AsOp(avaxAssetID)
		if err != nil {
			t.Skip("invalid tx")
		}

		oldTx := toOldTx(t, newTx)
		gasUsed, err := oldTx.UnsignedAtomicTx.GasUsed(true)
		require.NoErrorf(t, err, "%T.GasUsed(true)", oldTx.UnsignedAtomicTx)

		gasPrice, err := atomic.EffectiveGasPrice(oldTx.UnsignedAtomicTx, avaxAssetID, true)
		require.NoErrorf(t, err, "atomic.EffectiveGasPrice(%T, avaxAssetID, true)", oldTx)

		state := newAsOpStateDB()
		if export, ok := oldTx.UnsignedAtomicTx.(*atomic.UnsignedExportTx); ok {
			for _, in := range export.Ins {
				state.initialNonces[in.Address] = in.Nonce
			}
		}

		ctx := &snow.Context{AVAXAssetID: avaxAssetID}
		require.NoErrorf(t, oldTx.UnsignedAtomicTx.EVMStateTransfer(ctx, state), "%T.EVMStateTransfer()", oldTx.UnsignedAtomicTx)

		want := hook.Op{
			ID:        oldTx.ID(),
			Gas:       gas.Gas(gasUsed),
			GasFeeCap: gasPrice,
			Burn:      state.op.Burn,
			Mint:      state.op.Mint,
		}
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("%T.AsOp() diff (-want +got):\n%s", newTx, diff)
		}
	})
}

func TestAtomicRequests(t *testing.T) {
	tests := []struct {
		tx           golden
		wantChainID  ids.ID
		wantRequests *chainsatomic.Requests
	}{
		{
			tx:          importGolden,
			wantChainID: xChainID,
			wantRequests: &chainsatomic.Requests{
				RemoveRequests: [][]byte{
					common.FromHex("0xfd9e10917c4a2dab395683cfb766cdc584eba118bc22d3d0fc356fb79345cf64"),
				},
			},
		},
		{
			tx:          exportGolden,
			wantChainID: xChainID,
			wantRequests: &chainsatomic.Requests{
				PutRequests: []*chainsatomic.Element{{
					Key:   common.FromHex("0x38ebe8fc127b2eaeeb25c72a747e0ef27460fb04b5929568ed959d67ec3e4948"),
					Value: common.FromHex("0x000067b5812292324365c6e2a479b2601cd1cd1facc2fcc8c29d58b5ed96583ea17e0000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000000000000000100000000000000000000000100000001d6ce17826dd7c12a7577af257e82d99143b72500"),
					Traits: [][]byte{
						ids.ShortFromStringOrPanic("LanVZgBDVvtarbTXD1uU7r1nXVJyLmPUz").Bytes(),
					},
				}},
			},
		},
		{
			tx:          importMultiInputGolden,
			wantChainID: xChainID,
			wantRequests: &chainsatomic.Requests{
				RemoveRequests: [][]byte{
					common.FromHex("0x821514ed5d925142159bc2c78bc56b043200e53aab79e97ca75e7ca7f6a96d05"),
					common.FromHex("0xea05e5c7135613b689d9f6b9903f431067ed72a2957ca82a652de1e8fef2c630"),
					common.FromHex("0xd71fb48751f6d5732e7ff63168ed311b40bf517b36279e326878fc3f5169a656"),
				},
			},
		},
		{
			tx: exportSameAddressMultiAssetGolden,
			wantRequests: &chainsatomic.Requests{
				PutRequests: []*chainsatomic.Element{
					{
						Key:   common.FromHex("0x82c024362a71c075ac15e5e000dd66380907e3ea6af121d3d78478bb07848b75"),
						Value: common.FromHex("0x00005281e076436407df7c97e5abaff7f63e11bd8bc9ce03c787f12ee0e21fe68dca00000000000000000000000000000000000000000000000000000000000000000000000000000007000000000000006400000000000000000000000100000001aa00000000000000000000000000000000000000"),
						Traits: [][]byte{
							ids.ShortID{0xaa}.Bytes(),
						},
					},
					{
						Key:   common.FromHex("0x836913bdcf743940c51675ac186dc415cbb5f2a7309916f4a48bda3df5334245"),
						Value: common.FromHex("0x00005281e076436407df7c97e5abaff7f63e11bd8bc9ce03c787f12ee0e21fe68dca0000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000700000000000186a000000000000000000000000100000001aa00000000000000000000000000000000000000"),
						Traits: [][]byte{
							ids.ShortID{0xaa}.Bytes(),
						},
					},
				},
			},
		},
		{
			tx: exportMultiAddressMultiAssetGolden,
			wantRequests: &chainsatomic.Requests{
				PutRequests: []*chainsatomic.Element{
					{
						Key:   common.FromHex("0x150b950ce35ef7c512b1dec725164c8ee170728976ca0c9c1202eb60cafe9230"),
						Value: common.FromHex("0x0000d4ae9ab4d296ced15beba7686a2216bcfdf4a7f1bcc502b3e5c0c79a4601b2ef0000000000000000000000000000000000000000000000000000000000000000000000000000000700000000000001f400000000000000000000000200000002bb00000000000000000000000000000000000000cc00000000000000000000000000000000000000"),
						Traits: [][]byte{
							ids.ShortID{0xbb}.Bytes(),
							ids.ShortID{0xcc}.Bytes(),
						},
					},
					{
						Key:   common.FromHex("0x3080252925d9e3e399292cfecc8f95bb03da84645f44b8d0da82dc472e1f41d2"),
						Value: common.FromHex("0x0000d4ae9ab4d296ced15beba7686a2216bcfdf4a7f1bcc502b3e5c0c79a4601b2ef0000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000000000007a12000000000000000000000000200000002bb00000000000000000000000000000000000000cc00000000000000000000000000000000000000"),
						Traits: [][]byte{
							ids.ShortID{0xbb}.Bytes(),
							ids.ShortID{0xcc}.Bytes(),
						},
					},
				},
			},
		},
		{
			tx: importNonAVAXGolden,
			wantRequests: &chainsatomic.Requests{
				RemoveRequests: [][]byte{
					common.FromHex("0x2c34ce1df23b838c5abf2a7f6437cca3d3067ed509ff25f11df6b11b582b51eb"),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.tx.name, func(t *testing.T) {
			tx := test.tx.new
			gotChainID, gotRequests, err := tx.AtomicRequests()
			require.NoErrorf(t, err, "%T.AtomicRequests()", tx)
			assert.Equalf(t, test.wantChainID, gotChainID, "%T.AtomicRequests().ChainID", tx)
			assert.Equalf(t, test.wantRequests, gotRequests, "%T.AtomicRequests().Requests", tx)
		})
	}
}

func FuzzAtomicRequestsCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		oldTx := toOldTx(t, newTx)
		wantChainID, wantRequests, err := oldTx.UnsignedAtomicTx.AtomicOps()
		require.NoErrorf(t, err, "%T.AtomicOps()", oldTx.UnsignedAtomicTx)

		gotChainID, gotRequests, err := newTx.AtomicRequests()
		require.NoErrorf(t, err, "%T.AtomicRequests()", newTx)
		assert.Equal(t, wantChainID, gotChainID, "chainID")
		assert.Equal(t, wantRequests, gotRequests, "requests")
	})
}

func newEmptyStateDB(t testing.TB) *extstate.StateDB {
	t.Helper()

	db := state.NewDatabase(rawdb.NewMemoryDatabase())
	sdb, err := state.New(types.EmptyRootHash, db, nil)
	require.NoError(t, err)
	return extstate.New(sdb)
}

func compareStateDBs(want, got *extstate.StateDB) string {
	// Finalize the trie structures so that the state DB comparison includes
	// any changes.
	for _, v := range []*extstate.StateDB{want, got} {
		v.Finalise(true)
		v.IntermediateRoot(true)
	}

	opts := []cmp.Option{
		cmpopts.IgnoreUnexported(extstate.StateDB{}),
		cmputils.StateDBs(),
	}
	return cmp.Diff(want, got, opts...)
}

func TestTransferNonAVAX(t *testing.T) {
	var (
		alice = common.Address{1}
		bob   = common.Address{2}
		btc   = ids.ID{3}
		eth   = ids.ID{4}
	)
	tests := []struct {
		name    string
		init    map[common.Address]map[ids.ID]uint64
		tx      Unsigned
		want    map[common.Address]map[ids.ID]uint64
		wantErr error
	}{
		{
			name: "import_avax",
			tx: &Import{
				Outs: []Output{
					{Address: alice, Amount: 1, AssetID: avaxAssetID},
				},
			},
		},
		{
			name: "import_non_avax",
			tx: &Import{
				Outs: []Output{
					{Address: alice, Amount: 1, AssetID: btc},
				},
			},
			want: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 1,
				},
			},
		},
		{
			name: "import_non_avax_adds",
			init: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 1,
				},
			},
			tx: &Import{
				Outs: []Output{
					{Address: alice, Amount: 1, AssetID: btc},
				},
			},
			want: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 2,
				},
			},
		},
		{
			name: "import_many",
			tx: &Import{
				Outs: []Output{
					{Address: alice, Amount: 1, AssetID: avaxAssetID},
					{Address: alice, Amount: 10, AssetID: avaxAssetID},
					{Address: bob, Amount: 100, AssetID: avaxAssetID},
					{Address: alice, Amount: 1_000, AssetID: btc},
					{Address: alice, Amount: 10_000, AssetID: btc},
					{Address: bob, Amount: 100_000, AssetID: btc},
					{Address: bob, Amount: 1_000_000, AssetID: eth},
				},
			},
			want: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 11_000,
				},
				bob: {
					btc: 100_000,
					eth: 1_000_000,
				},
			},
		},
		{
			name: "export_avax",
			tx: &Export{
				Ins: []Input{
					{Address: alice, Amount: 1, AssetID: avaxAssetID},
				},
			},
		},
		{
			name: "export_non_avax",
			init: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 2,
				},
			},
			tx: &Export{
				Ins: []Input{
					{Address: alice, Amount: 1, AssetID: btc},
				},
			},
			want: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 1,
				},
			},
		},
		{
			name: "export_many",
			init: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 22_000,
				},
				bob: {
					btc: 200_000,
					eth: 2_000_000,
				},
			},
			tx: &Export{
				Ins: []Input{
					{Address: alice, Amount: 1, AssetID: avaxAssetID},
					{Address: alice, Amount: 10, AssetID: avaxAssetID},
					{Address: bob, Amount: 100, AssetID: avaxAssetID},
					{Address: alice, Amount: 1_000, AssetID: btc},
					{Address: alice, Amount: 10_000, AssetID: btc},
					{Address: bob, Amount: 100_000, AssetID: btc},
					{Address: bob, Amount: 1_000_000, AssetID: eth},
				},
			},
			want: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 11_000,
				},
				bob: {
					btc: 100_000,
					eth: 1_000_000,
				},
			},
		},
		{
			name: "export_non_avax_insufficient",
			tx: &Export{
				Ins: []Input{
					{Address: alice, Amount: 1, AssetID: btc},
				},
			},
			wantErr: ErrInsufficientFunds,
		},
		{
			name: "export_non_avax_total_insufficient",
			init: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 2,
				},
			},
			tx: &Export{
				Ins: []Input{
					{Address: alice, Amount: 1, AssetID: btc},
					{Address: alice, Amount: 2, AssetID: btc},
				},
			},
			want: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 1,
				},
			},
			wantErr: ErrInsufficientFunds,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				want  = newEmptyStateDB(t)
				toBig = func(v uint64) *big.Int { return new(big.Int).SetUint64(v) }
			)
			for addr, balances := range test.want {
				for assetID, amount := range balances {
					coinID := common.Hash(assetID)
					want.AddBalanceMultiCoin(addr, coinID, toBig(amount))
				}
			}

			got := newEmptyStateDB(t)
			for addr, balances := range test.init {
				for assetID, amount := range balances {
					coinID := common.Hash(assetID)
					got.AddBalanceMultiCoin(addr, coinID, toBig(amount))
				}
			}

			tx := &Tx{
				Unsigned: test.tx,
			}
			err := tx.TransferNonAVAX(avaxAssetID, got)
			require.ErrorIs(t, err, test.wantErr)
			if diff := compareStateDBs(want, got); diff != "" {
				t.Errorf("%T.TransferNonAVAX() diff (-want +got):\n%s", tx, diff)
			}
		})
	}
}

func FuzzTransferNonAVAXCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		op, err := newTx.AsOp(avaxAssetID)
		if err != nil {
			t.Skip("invalid tx")
		}

		oldState := newEmptyStateDB(t)
		newState := newEmptyStateDB(t)
		states := []*extstate.StateDB{oldState, newState}

		if tx, ok := newTx.Unsigned.(*Export); ok {
			for _, in := range tx.Ins {
				// Coreth silently overflows the nonce, whereas SAE will leave
				// the nonce unmodified. This difference doesn't matter on live
				// networks.
				if in.Nonce == math.MaxUint64 {
					t.Skip("nonce overflow")
				}

				for _, state := range states {
					state.AddBalance(in.Address, largeUint256())
					state.SetNonce(in.Address, in.Nonce)
					state.AddBalanceMultiCoin(in.Address, common.Hash(in.AssetID), largeBigInt())
				}
			}
		}

		var (
			oldTx = toOldTx(t, newTx)
			ctx   = &snow.Context{AVAXAssetID: avaxAssetID}
		)
		require.NoErrorf(t, oldTx.EVMStateTransfer(ctx, oldState), "%T.EVMStateTransfer()", oldTx)
		require.NoErrorf(t, newTx.TransferNonAVAX(avaxAssetID, newState), "%T.TransferNonAVAX()", newTx)
		require.NoErrorf(t, op.ApplyTo(newState.StateDB), "%T.ApplyTo(%T)", op, newState.StateDB)

		if diff := compareStateDBs(oldState, newState); diff != "" {
			t.Errorf("%T.TransferNonAVAX() diff (-want +got):\n%s", newTx, diff)
		}
	})
}

// mainnetContext returns a [snow.Context] with mainnet values.
func mainnetContext() *snow.Context {
	return &snow.Context{
		NetworkID:   constants.MainnetID,
		SubnetID:    constants.PrimaryNetworkID,
		ChainID:     cChainID,
		XChainID:    xChainID,
		CChainID:    cChainID,
		AVAXAssetID: avaxAssetID,
		ValidatorState: &validatorstest.State{
			GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
				switch chainID {
				case constants.PlatformChainID, xChainID, cChainID:
					return constants.PrimaryNetworkID, nil
				default:
					return ids.GenerateTestID(), nil
				}
			},
		},
	}
}

func TestSanityCheck(t *testing.T) {
	var (
		ctx     = mainnetContext()
		nonAVAX = ids.ID{1}

		validImport = func() *Import {
			return &Import{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				SourceChain:  ctx.XChainID,
				ImportedInputs: []*avax.TransferableInput{{
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 100,
					},
				}},
				Outs: []Output{{
					Amount:  100,
					AssetID: ctx.AVAXAssetID,
				}},
			}
		}
		imp = func(mutate func(*Import)) Unsigned {
			i := validImport()
			mutate(i)
			return i
		}

		validExport = func() *Export {
			return &Export{
				NetworkID:        ctx.NetworkID,
				BlockchainID:     ctx.ChainID,
				DestinationChain: ctx.XChainID,
				Ins: []Input{{
					Amount:  100,
					AssetID: ctx.AVAXAssetID,
				}},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					Out:   &secp256k1fx.TransferOutput{Amt: 100},
				}},
			}
		}
		exp = func(mutate func(*Export)) Unsigned {
			e := validExport()
			mutate(e)
			return e
		}
	)
	tests := []struct {
		name    string
		tx      Unsigned
		wantErr error
	}{
		{
			name: "import_valid",
			tx:   validImport(),
		},
		{
			name: "import_valid_pchain",
			tx:   imp(func(i *Import) { i.SourceChain = constants.PlatformChainID }),
		},
		{
			name: "import_mainnet",
			tx:   importGolden.new.Unsigned,
		},
		{
			name:    "import_wrong_network_id",
			tx:      imp(func(i *Import) { i.NetworkID++ }),
			wantErr: ErrWrongNetworkID,
		},
		{
			name:    "import_wrong_chain_id",
			tx:      imp(func(i *Import) { i.BlockchainID = xChainID }),
			wantErr: ErrWrongChainID,
		},
		{
			name:    "import_wrong_source_chain",
			tx:      imp(func(i *Import) { i.SourceChain = cChainID }),
			wantErr: ErrNotSameSubnet,
		},
		{
			name:    "import_no_inputs",
			tx:      imp(func(i *Import) { i.ImportedInputs = nil }),
			wantErr: ErrNoInputs,
		},
		{
			name:    "import_no_outputs",
			tx:      imp(func(i *Import) { i.Outs = nil }),
			wantErr: ErrNoOutputs,
		},
		{
			name:    "import_invalid_input",
			tx:      imp(func(i *Import) { i.ImportedInputs[0].In.(*secp256k1fx.TransferInput).Amt = 0 }),
			wantErr: ErrInvalidInput,
		},
		{
			name:    "import_non_avax_input",
			tx:      imp(func(i *Import) { i.ImportedInputs[0].Asset.ID = nonAVAX }),
			wantErr: ErrNonAVAXInput,
		},
		{
			name:    "import_zero_amount_output",
			tx:      imp(func(i *Import) { i.Outs[0].Amount = 0 }),
			wantErr: ErrInvalidOutput,
		},
		{
			name:    "import_non_avax_output",
			tx:      imp(func(i *Import) { i.Outs[0].AssetID = nonAVAX }),
			wantErr: ErrNonAVAXOutput,
		},
		{
			name:    "import_flow_check_failed",
			tx:      imp(func(i *Import) { i.Outs[0].Amount = 200 }),
			wantErr: ErrFlowCheckFailed,
		},
		{
			name: "import_inputs_not_unique",
			tx: imp(func(i *Import) {
				i.ImportedInputs = []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{1}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 50,
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{1}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 50,
						},
					},
				}
			}),
			wantErr: ErrInputsNotSortedUnique,
		},
		{
			name: "import_inputs_not_sorted",
			tx: imp(func(i *Import) {
				i.ImportedInputs = []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{2}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 50,
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{1}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 50,
						},
					},
				}
			}),
			wantErr: ErrInputsNotSortedUnique,
		},
		{
			name: "import_outputs_not_unique",
			tx: imp(func(i *Import) {
				i.Outs = []Output{
					{Amount: 50, AssetID: ctx.AVAXAssetID},
					{Amount: 50, AssetID: ctx.AVAXAssetID},
				}
			}),
			wantErr: ErrOutputsNotSortedUnique,
		},
		{
			name: "import_outputs_not_sorted",
			tx: imp(func(i *Import) {
				i.Outs = []Output{
					{Address: common.Address{2}, Amount: 50, AssetID: ctx.AVAXAssetID},
					{Address: common.Address{1}, Amount: 50, AssetID: ctx.AVAXAssetID},
				}
			}),
			wantErr: ErrOutputsNotSortedUnique,
		},
		{
			name: "export_valid",
			tx:   validExport(),
		},
		{
			name: "export_valid_pchain",
			tx:   exp(func(e *Export) { e.DestinationChain = constants.PlatformChainID }),
		},
		{
			name: "export_valid_outputs_not_unique",
			tx: exp(func(e *Export) {
				e.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out:   &secp256k1fx.TransferOutput{Amt: 50},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out:   &secp256k1fx.TransferOutput{Amt: 50},
					},
				}
			}),
		},
		{
			name: "export_mainnet",
			tx:   exportGolden.new.Unsigned,
		},
		{
			name:    "export_wrong_network_id",
			tx:      exp(func(e *Export) { e.NetworkID++ }),
			wantErr: ErrWrongNetworkID,
		},
		{
			name:    "export_wrong_chain_id",
			tx:      exp(func(e *Export) { e.BlockchainID = xChainID }),
			wantErr: ErrWrongChainID,
		},
		{
			name:    "export_wrong_destination_chain",
			tx:      exp(func(e *Export) { e.DestinationChain = cChainID }),
			wantErr: ErrNotSameSubnet,
		},
		{
			name:    "export_no_inputs",
			tx:      exp(func(e *Export) { e.Ins = nil }),
			wantErr: ErrNoInputs,
		},
		{
			name:    "export_no_outputs",
			tx:      exp(func(e *Export) { e.ExportedOutputs = nil }),
			wantErr: ErrNoOutputs,
		},
		{
			name:    "export_zero_amount_input",
			tx:      exp(func(e *Export) { e.Ins[0].Amount = 0 }),
			wantErr: ErrInvalidInput,
		},
		{
			name:    "export_non_avax_input",
			tx:      exp(func(e *Export) { e.Ins[0].AssetID = nonAVAX }),
			wantErr: ErrNonAVAXInput,
		},
		{
			name:    "export_invalid_output",
			tx:      exp(func(e *Export) { e.ExportedOutputs[0].Out.(*secp256k1fx.TransferOutput).Amt = 0 }),
			wantErr: ErrInvalidOutput,
		},
		{
			name:    "export_non_avax_output",
			tx:      exp(func(e *Export) { e.ExportedOutputs[0].Asset.ID = nonAVAX }),
			wantErr: ErrNonAVAXOutput,
		},
		{
			name:    "export_flow_check_failed",
			tx:      exp(func(e *Export) { e.ExportedOutputs[0].Out.(*secp256k1fx.TransferOutput).Amt = 200 }),
			wantErr: ErrFlowCheckFailed,
		},
		{
			name: "export_inputs_not_unique",
			tx: exp(func(e *Export) {
				e.Ins = []Input{
					{Amount: 50, AssetID: ctx.AVAXAssetID},
					{Amount: 50, AssetID: ctx.AVAXAssetID},
				}
			}),
			wantErr: ErrInputsNotSortedUnique,
		},
		{
			name: "export_inputs_not_sorted",
			tx: exp(func(e *Export) {
				e.Ins = []Input{
					{Address: common.Address{2}, Amount: 50, AssetID: ctx.AVAXAssetID},
					{Address: common.Address{1}, Amount: 50, AssetID: ctx.AVAXAssetID},
				}
			}),
			wantErr: ErrInputsNotSortedUnique,
		},
		{
			name: "export_outputs_not_sorted",
			tx: exp(func(e *Export) {
				e.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out:   &secp256k1fx.TransferOutput{Amt: 75},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out:   &secp256k1fx.TransferOutput{Amt: 25},
					},
				}
			}),
			wantErr: ErrOutputsNotSorted,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &Tx{
				Unsigned: test.tx,
			}
			err := tx.SanityCheck(ctx)
			require.ErrorIsf(t, err, test.wantErr, "%T.SanityCheck()", tx)
		})
	}
}

// oldSanityCheck behaves like [Tx.SanityCheck] for the legacy [atomic.Tx].
func oldSanityCheck(tx *atomic.Tx, ctx *snow.Context) error {
	rules := *extrastest.ForkToRules(upgradetest.Latest)
	if err := tx.UnsignedAtomicTx.Verify(ctx, rules); err != nil {
		return err
	}
	// We can't call [vm.VerifierBackend.SemanticVerify] here because that
	// additionally performs signature verification.
	fc := avax.NewFlowChecker()
	switch tx := tx.UnsignedAtomicTx.(type) {
	case *atomic.UnsignedImportTx:
		for _, in := range tx.ImportedInputs {
			fc.Consume(in.Asset.ID, in.Input().Amount())
		}
		for _, out := range tx.Outs {
			fc.Produce(out.AssetID, out.Amount)
		}
	case *atomic.UnsignedExportTx:
		for _, in := range tx.Ins {
			fc.Consume(in.AssetID, in.Amount)
		}
		for _, out := range tx.ExportedOutputs {
			fc.Produce(out.Asset.ID, out.Output().Amount())
		}
	}
	return fc.Verify()
}

func FuzzSanityCheckCompatibility(f *testing.F) {
	ctx := mainnetContext()
	fuzz(f, func(t *testing.T, newTx *Tx) {
		oldTx := toOldTx(t, newTx)
		want := oldSanityCheck(oldTx, ctx) == nil
		got := newTx.SanityCheck(ctx) == nil
		assert.Equal(t, want, got, "%T.SanityCheck() == OldSanityCheck(%T)", newTx, oldTx)
	})
}

func TestVerifyCredentials(t *testing.T) {
	sk, err := secp256k1.ToPrivateKey(common.FromHex("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"))
	require.NoError(t, err, "secp256k1.ToPrivateKey()")
	var (
		ethAddress  = sk.EthAddress()
		avaxAddress = sk.Address()

		validUTXOID  = avax.UTXOID{TxID: ids.ID{1}}
		validInputID = validUTXOID.InputID()

		validImportTx = func() *Tx {
			tx := &Import{
				SourceChain: xChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: validUTXOID,
					Asset:  avax.Asset{ID: avaxAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   100,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
			}
			return &Tx{
				Unsigned: tx,
				Creds: []Credential{&secp256k1fx.Credential{Sigs: []txtest.Signature{
					txtest.Sign(t, tx, sk),
				}}},
			}
		}
		imp = func(mutate func(*Tx)) *Tx {
			tx := validImportTx()
			mutate(tx)
			return tx
		}
		validUTXOs = func(t *testing.T) []*chainsatomic.Element {
			t.Helper()

			utxo := &avax.UTXO{
				UTXOID: validUTXOID,
				Asset:  avax.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 100,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{avaxAddress},
					},
				},
			}
			return []*chainsatomic.Element{{
				Key:   validInputID[:],
				Value: MarshalUTXO(t, utxo),
			}}
		}(t)

		validExportTx = func() *Tx {
			tx := &Export{
				Ins: []Input{
					{Address: ethAddress, Amount: 100, AssetID: avaxAssetID},
				},
			}
			return &Tx{
				Unsigned: tx,
				Creds: []Credential{&secp256k1fx.Credential{Sigs: []txtest.Signature{
					txtest.Sign(t, tx, sk),
				}}},
			}
		}
		exp = func(mutate func(*Tx)) *Tx {
			tx := validExportTx()
			mutate(tx)
			return tx
		}
	)

	tests := []struct {
		name    string
		tx      *Tx
		utxos   []*chainsatomic.Element
		wantErr error
	}{
		{
			name:  "import_valid",
			tx:    validImportTx(),
			utxos: validUTXOs,
		},
		{
			name:    "import_wrong_num_credentials",
			tx:      imp(func(tx *Tx) { tx.Creds = nil }),
			utxos:   validUTXOs,
			wantErr: ErrIncorrectNumCredentials,
		},
		{
			name: "import_unserializable",
			tx: imp(func(tx *Tx) {
				tx.Unsigned.(*Import).ImportedInputs = make([]*avax.TransferableInput, 1)
			}),
			wantErr: ErrConvertingToFxTx,
		},
		{
			name:    "import_missing_utxo",
			tx:      validImportTx(),
			wantErr: ErrFetchingUTXOs,
		},
		{
			name: "import_unmarshalling_utxo",
			tx:   validImportTx(),
			utxos: []*chainsatomic.Element{{
				Key:   validInputID[:],
				Value: []byte{0xff, 0xff, 0xff},
			}},
			wantErr: ErrUnmarshallingUTXO,
		},
		{
			name: "import_mismatched_asset_ids",
			tx: imp(func(tx *Tx) {
				tx.Unsigned.(*Import).ImportedInputs[0].Asset.ID[0]++
			}),
			utxos:   validUTXOs,
			wantErr: ErrMismatchedAssetIDs,
		},
		{
			name: "import_invalid_signature",
			tx: imp(func(tx *Tx) {
				tx.Creds = []Credential{&secp256k1fx.Credential{Sigs: []txtest.Signature{{}}}}
			}),
			utxos:   validUTXOs,
			wantErr: ErrVerifyingTransfer,
		},
		{
			name: "export_valid",
			tx:   validExportTx(),
		},
		{
			name:    "export_no_credential",
			tx:      exp(func(tx *Tx) { tx.Creds = nil }),
			wantErr: ErrIncorrectNumCredentials,
		},
		{
			name: "export_unserializable",
			tx: exp(func(tx *Tx) {
				tx.Unsigned.(*Export).ExportedOutputs = make([]*avax.TransferableOutput, 1)
			}),
			wantErr: ErrConvertingToFxTx,
		},
		{
			name: "export_no_signature",
			tx: exp(func(tx *Tx) {
				tx.Creds = []Credential{&secp256k1fx.Credential{Sigs: nil}}
			}),
			wantErr: ErrIncorrectNumSignatures,
		},
		{
			name: "export_invalid_signature",
			tx: exp(func(tx *Tx) {
				tx.Creds = []Credential{&secp256k1fx.Credential{Sigs: []txtest.Signature{{}}}}
			}),
			wantErr: ErrRecoveringPublicKey,
		},
		{
			name: "export_address_mismatch",
			tx: exp(func(tx *Tx) {
				tx.Unsigned.(*Export).Ins[0].Address = common.Address{}
			}),
			wantErr: ErrAddressMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			memory := chainsatomic.NewMemory(memdb.New())
			xMemory := memory.NewSharedMemory(xChainID)
			err := xMemory.Apply(map[ids.ID]*chainsatomic.Requests{
				cChainID: {PutRequests: test.utxos},
			})
			require.NoErrorf(t, err, "%T.Apply()", xMemory)

			cMemory := memory.NewSharedMemory(cChainID)
			err = test.tx.VerifyCredentials(cMemory)
			assert.ErrorIsf(t, err, test.wantErr, "%T.VerifyCredentials()", test.tx)
		})
	}
}
