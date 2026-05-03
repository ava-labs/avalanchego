// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"context"
	"encoding/json"
	"errors"
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

	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"

	// Imported for [vm.VerifierBackend] comment resolution.
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras/extrastest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

// Tests is defined at the package level to allow sharing between fuzz tests and
// unit tests.
var (
	AVAXAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
	CChainID    = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	XChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	Tests       = [...]struct {
		Name                  string
		Old                   *atomic.Tx
		New                   *Tx
		JSON                  string
		Bytes                 []byte
		Op                    hook.Op
		AtomicRequestsChainID ids.ID
		AtomicRequests        *chainsatomic.Requests
		InputIDs              set.Set[ids.ID]
	}{
		{
			Name: "import", // Included in https://subnets.avax.network/c-chain/block/4
			Old: &atomic.Tx{
				UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    constants.MainnetID,
					BlockchainID: CChainID,
					SourceChain:  XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{
							TxID:        ids.FromStringOrPanic("2VqSFA5hxukiv1FSAB8ShjwHwmPev9ZS8VD9aUTCDRoff7T5Bi"),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: AVAXAssetID,
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
						AssetID: AVAXAssetID,
					}},
				},
				Creds: []verify.Verifiable{
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x3e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01")),
						},
					},
				},
			},
			New: &Tx{
				Unsigned: &Import{
					NetworkID:    constants.MainnetID,
					BlockchainID: CChainID,
					SourceChain:  XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{
							TxID:        ids.FromStringOrPanic("2VqSFA5hxukiv1FSAB8ShjwHwmPev9ZS8VD9aUTCDRoff7T5Bi"),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: AVAXAssetID,
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
						AssetID: AVAXAssetID,
					}},
				},
				Creds: []Credential{
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x3e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01")),
						},
					},
				},
			},
			JSON: `{
				"unsignedTx":{
					"networkID":1,
					"blockchainID":"2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
					"sourceChain":"2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
					"importedInputs":[{
						"txID":"2VqSFA5hxukiv1FSAB8ShjwHwmPev9ZS8VD9aUTCDRoff7T5Bi",
						"outputIndex":1,
						"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
						"fxID":"11111111111111111111111111111111LpoYY",
						"input":{
							"amount":50000000,
							"signatureIndices":[0]
						}
					}],
					"outputs":[{
						"address":"0xb8b5a87d1c05676f1f966da49151fa54dbe68c33",
						"amount":50000000,
						"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"
					}]
				},
				"credentials":[{
					"signatures":[
						"0x3e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01"
					]
				}]
			}`,
			Bytes: common.FromHex("0x000000000000000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b00000001c52b712aa7dce27a650bf509f799673e245edd4fa9e4e1700eb6105202fe579a0000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000002faf080000000010000000000000001b8b5a87d1c05676f1f966da49151fa54dbe68c330000000002faf08021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000100000009000000013e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01"),
			Op: hook.Op{
				ID:  ids.FromStringOrPanic("h34BPNmYApCbW8buVWAtzu1KtjTFmyMhiRQQnAqPqwCqQsB7f"),
				Gas: 11230,
				Mint: map[common.Address]uint256.Int{
					common.HexToAddress("0xb8b5a87d1c05676f1f966da49151fa54dbe68c33"): scaleAVAX(50_000_000),
				},
			},
			AtomicRequestsChainID: XChainID,
			AtomicRequests: &chainsatomic.Requests{
				RemoveRequests: [][]byte{
					common.FromHex("0xfd9e10917c4a2dab395683cfb766cdc584eba118bc22d3d0fc356fb79345cf64"),
				},
			},
			InputIDs: set.Of(
				ids.ID(common.FromHex("0xfd9e10917c4a2dab395683cfb766cdc584eba118bc22d3d0fc356fb79345cf64")),
			),
		},
		{
			Name: "export", // Included in https://subnets.avax.network/c-chain/block/48
			Old: &atomic.Tx{
				UnsignedAtomicTx: &atomic.UnsignedExportTx{
					NetworkID:        constants.MainnetID,
					BlockchainID:     CChainID,
					DestinationChain: XChainID,
					Ins: []atomic.EVMInput{{
						Address: common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"),
						Amount:  1000001,
						AssetID: AVAXAssetID,
					}},
					ExportedOutputs: []*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: AVAXAssetID,
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
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01")),
						},
					},
				},
			},
			New: &Tx{
				Unsigned: &Export{
					NetworkID:        constants.MainnetID,
					BlockchainID:     CChainID,
					DestinationChain: XChainID,
					Ins: []Input{{
						Address: common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"),
						Amount:  1000001,
						AssetID: AVAXAssetID,
					}},
					ExportedOutputs: []*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: AVAXAssetID,
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
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01")),
						},
					},
				},
			},
			JSON: `{
				"unsignedTx":{
					"networkID":1,
					"blockchainID":"2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
					"destinationChain":"2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
					"inputs":[{
						"address":"0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6",
						"amount":1000001,
						"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
						"nonce":0
					}],
					"exportedOutputs":[{
						"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
						"fxID":"11111111111111111111111111111111LpoYY",
						"output":{
							"addresses":["LanVZgBDVvtarbTXD1uU7r1nXVJyLmPUz"],
							"amount":1,
							"locktime":0,
							"threshold":1
						}
					}]
				},
				"credentials":[{
					"signatures":[
						"0x254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01"
					]
				}]
			}`,
			Bytes: common.FromHex("0x000000000001000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b00000001eb019ccd325ad53543a7e7e3b04828bdecf3cff600000000000f424121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000000000000000000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000000000000000100000000000000000000000100000001d6ce17826dd7c12a7577af257e82d99143b72500000000010000000900000001254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01"),
			Op: hook.Op{
				ID:        ids.FromStringOrPanic("ng7Dox1r8nctrF6zurhRPYWxkmE2juUhT7Qhpauyo8qSEu6jB"),
				Gas:       11230,
				GasFeeCap: *uint256.NewInt(1_000_000 * _x2cRate / 11230),
				Burn: map[common.Address]hook.AccountDebit{
					common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"): {
						Amount:     scaleAVAX(1_000_001),
						MinBalance: scaleAVAX(1_000_001),
					},
				},
			},
			AtomicRequestsChainID: XChainID,
			AtomicRequests: &chainsatomic.Requests{
				PutRequests: []*chainsatomic.Element{{
					Key:   common.FromHex("0x38ebe8fc127b2eaeeb25c72a747e0ef27460fb04b5929568ed959d67ec3e4948"),
					Value: common.FromHex("0x000067b5812292324365c6e2a479b2601cd1cd1facc2fcc8c29d58b5ed96583ea17e0000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000000000000000100000000000000000000000100000001d6ce17826dd7c12a7577af257e82d99143b72500"),
					Traits: [][]byte{
						ids.ShortFromStringOrPanic("LanVZgBDVvtarbTXD1uU7r1nXVJyLmPUz").Bytes(),
					},
				}},
			},
			InputIDs: set.Of(
				ids.ID(common.FromHex("0x000000000000000000000014eb019ccd325ad53543a7e7e3b04828bdecf3cff6")),
			),
		},
		{
			Name: "import_multi_input", // Included in https://subnets.avax.network/c-chain/block/132481
			Old: &atomic.Tx{
				UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    constants.MainnetID,
					BlockchainID: CChainID,
					SourceChain:  XChainID,
					ImportedInputs: []*avax.TransferableInput{
						{
							UTXOID: avax.UTXOID{
								TxID: ids.FromStringOrPanic("DqRKjysHeiKWetgyqqM2WdnX56yg8wBdY95RhuP3eDbbVoMCH"),
							},
							Asset: avax.Asset{ID: AVAXAssetID},
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
							Asset: avax.Asset{ID: AVAXAssetID},
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
							Asset: avax.Asset{ID: AVAXAssetID},
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
							AssetID: AVAXAssetID,
						},
						{
							Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
							Amount:  99000000,
							AssetID: AVAXAssetID,
						},
						{
							Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
							Amount:  399000000,
							AssetID: AVAXAssetID,
						},
					},
				},
				Creds: []verify.Verifiable{
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
						},
					},
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
						},
					},
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
						},
					},
				},
			},
			New: &Tx{
				Unsigned: &Import{
					NetworkID:    constants.MainnetID,
					BlockchainID: CChainID,
					SourceChain:  XChainID,
					ImportedInputs: []*avax.TransferableInput{
						{
							UTXOID: avax.UTXOID{
								TxID: ids.FromStringOrPanic("DqRKjysHeiKWetgyqqM2WdnX56yg8wBdY95RhuP3eDbbVoMCH"),
							},
							Asset: avax.Asset{ID: AVAXAssetID},
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
							Asset: avax.Asset{ID: AVAXAssetID},
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
							Asset: avax.Asset{ID: AVAXAssetID},
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
							AssetID: AVAXAssetID,
						},
						{
							Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
							Amount:  99000000,
							AssetID: AVAXAssetID,
						},
						{
							Address: common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"),
							Amount:  399000000,
							AssetID: AVAXAssetID,
						},
					},
				},
				Creds: []Credential{
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
						},
					},
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
						},
					},
					&secp256k1fx.Credential{
						Sigs: [][65]byte{
							[65]byte(common.FromHex("0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700")),
						},
					},
				},
			},
			JSON: `{
				"unsignedTx":{
					"networkID":1,
					"blockchainID":"2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
					"sourceChain":"2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
					"importedInputs":[
						{
							"txID":"DqRKjysHeiKWetgyqqM2WdnX56yg8wBdY95RhuP3eDbbVoMCH",
							"outputIndex":0,
							"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
							"fxID":"11111111111111111111111111111111LpoYY",
							"input":{"amount":99000000,"signatureIndices":[0]}
						},
						{
							"txID":"25YuXY1zoYY3DgLsRbGjdNSx3jYtvqZRgFo6jpy7EMCfUn4S74",
							"outputIndex":0,
							"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
							"fxID":"11111111111111111111111111111111LpoYY",
							"input":{"amount":399000000,"signatureIndices":[0]}
						},
						{
							"txID":"2DXSj1kzqWM5HWS2PXcDSD3GUNpEGinynV1qD6LxiECHmZC8fj",
							"outputIndex":0,
							"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
							"fxID":"11111111111111111111111111111111LpoYY",
							"input":{"amount":99000000,"signatureIndices":[0]}
						}
					],
					"outputs":[
						{"address":"0x383c293db6be7ac246f0956ad632344dc2cd1da3","amount":99000000,"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"},
						{"address":"0x383c293db6be7ac246f0956ad632344dc2cd1da3","amount":99000000,"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"},
						{"address":"0x383c293db6be7ac246f0956ad632344dc2cd1da3","amount":399000000,"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"}
					]
				},
				"credentials":[
					{"signatures":["0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700"]},
					{"signatures":["0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700"]},
					{"signatures":["0x4e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700"]}
				]
			}`,
			Bytes: common.FromHex("0x000000000000000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b000000031d249d0aab138afe01e6eff9c4789018a600771d94f5396b5df7b9d05298714d0000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000005e69ec000000001000000008e0713e47bfc29bef4cee6e4635da1c74a3aabade68ccad6fca3e99fd827eb1c0000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000017c841c00000000100000000a022a8b069a5d5e54c7e09c5c5b0f762c6751068bef15fe951a5e4b349d642200000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000005e69ec0000000010000000000000003383c293db6be7ac246f0956ad632344dc2cd1da30000000005e69ec021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff383c293db6be7ac246f0956ad632344dc2cd1da30000000005e69ec021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff383c293db6be7ac246f0956ad632344dc2cd1da30000000017c841c021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000300000009000000014e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b342570000000009000000014e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b342570000000009000000014e14b32cb790fdccc3ee4700c84d0d53986ea8f125bd69ce771d9db45f86705c48b01bbe763dddea3d27069ed12f9b3050c9dcd487830d03d6a4d90e21b3425700"),
			Op: hook.Op{
				ID:  ids.FromStringOrPanic("2Av7bXLRwxiQhbT9EcQd8KRM3Lz6VkpTqf3Y1AT5peHZ4YAohS"),
				Gas: 13526,
				Mint: map[common.Address]uint256.Int{
					common.HexToAddress("0x383c293db6be7ac246f0956ad632344dc2cd1da3"): scaleAVAX(597_000_000),
				},
			},
			AtomicRequestsChainID: XChainID,
			AtomicRequests: &chainsatomic.Requests{
				RemoveRequests: [][]byte{
					common.FromHex("0x821514ed5d925142159bc2c78bc56b043200e53aab79e97ca75e7ca7f6a96d05"),
					common.FromHex("0xea05e5c7135613b689d9f6b9903f431067ed72a2957ca82a652de1e8fef2c630"),
					common.FromHex("0xd71fb48751f6d5732e7ff63168ed311b40bf517b36279e326878fc3f5169a656"),
				},
			},
			InputIDs: set.Of(
				ids.ID(common.FromHex("0x821514ed5d925142159bc2c78bc56b043200e53aab79e97ca75e7ca7f6a96d05")),
				ids.ID(common.FromHex("0xea05e5c7135613b689d9f6b9903f431067ed72a2957ca82a652de1e8fef2c630")),
				ids.ID(common.FromHex("0xd71fb48751f6d5732e7ff63168ed311b40bf517b36279e326878fc3f5169a656")),
			),
		},
		{
			Name: "export_same_address_multi_asset", // Synthetic
			Old: &atomic.Tx{
				UnsignedAtomicTx: &atomic.UnsignedExportTx{
					Ins: []atomic.EVMInput{
						{
							Amount: 999,
							Nonce:  5,
						},
						{
							Amount:  1_000_000,
							AssetID: AVAXAssetID,
							Nonce:   5,
						},
					},
					ExportedOutputs: []*avax.TransferableOutput{},
				},
				Creds: []verify.Verifiable{},
			},
			New: &Tx{
				Unsigned: &Export{
					Ins: []Input{
						{
							Amount: 999,
							Nonce:  5,
						},
						{
							Amount:  1_000_000,
							AssetID: AVAXAssetID,
							Nonce:   5,
						},
					},
					ExportedOutputs: []*avax.TransferableOutput{},
				},
				Creds: []Credential{},
			},
			JSON: `{
				"unsignedTx":{
					"networkID":0,
					"blockchainID":"11111111111111111111111111111111LpoYY",
					"destinationChain":"11111111111111111111111111111111LpoYY",
					"inputs":[
						{"address":"0x0000000000000000000000000000000000000000","amount":999,"assetID":"11111111111111111111111111111111LpoYY","nonce":5},
						{"address":"0x0000000000000000000000000000000000000000","amount":1000000,"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z","nonce":5}
					],
					"exportedOutputs":[]
				},
				"credentials":[]
			}`,
			Bytes: common.FromHex("0x000000000001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000003e700000000000000000000000000000000000000000000000000000000000000000000000000000005000000000000000000000000000000000000000000000000000f424021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000000000000050000000000000000"),
			Op: hook.Op{
				ID:        ids.FromStringOrPanic("29cCETWxEUN1QCuex59j46Xtr8urBRo5M7HzwBqC3qDXWd73sX"),
				Gas:       12218,
				GasFeeCap: *uint256.NewInt(1_000_000 * _x2cRate / 12218),
				Burn: map[common.Address]hook.AccountDebit{
					{}: {
						Nonce:      5,
						Amount:     scaleAVAX(1_000_000),
						MinBalance: scaleAVAX(1_000_000),
					},
				},
			},
			AtomicRequests: &chainsatomic.Requests{
				PutRequests: []*chainsatomic.Element{},
			},
			InputIDs: set.Of(
				ids.ID(common.FromHex("0x0000000000000005000000140000000000000000000000000000000000000000")),
			),
		},
		{
			Name: "export_multi_address_multi_asset", // Synthetic
			Old: &atomic.Tx{
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
							AssetID: AVAXAssetID,
							Nonce:   7,
						},
					},
					ExportedOutputs: []*avax.TransferableOutput{},
				},
				Creds: []verify.Verifiable{},
			},
			New: &Tx{
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
							AssetID: AVAXAssetID,
							Nonce:   7,
						},
					},
					ExportedOutputs: []*avax.TransferableOutput{},
				},
				Creds: []Credential{},
			},
			JSON: `{
				"unsignedTx":{
					"networkID":0,
					"blockchainID":"11111111111111111111111111111111LpoYY",
					"destinationChain":"11111111111111111111111111111111LpoYY",
					"inputs":[
						{"address":"0x0100000000000000000000000000000000000000","amount":999,"assetID":"11111111111111111111111111111111LpoYY","nonce":5},
						{"address":"0x0200000000000000000000000000000000000000","amount":1000000,"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z","nonce":7}
					],
					"exportedOutputs":[]
				},
				"credentials":[]
			}`,
			Bytes: common.FromHex("0x000000000001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002010000000000000000000000000000000000000000000000000003e700000000000000000000000000000000000000000000000000000000000000000000000000000005020000000000000000000000000000000000000000000000000f424021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000000000000070000000000000000"),
			Op: hook.Op{
				ID:        ids.FromStringOrPanic("8P9XRKhxHeTv3t4Aj9cTV6dD5h78WVFH8nctLuCkeSavfKeEG"),
				Gas:       12218,
				GasFeeCap: *uint256.NewInt(1_000_000 * _x2cRate / 12218),
				Burn: map[common.Address]hook.AccountDebit{
					{1}: {
						Nonce: 5,
					},
					{2}: {
						Nonce:      7,
						Amount:     scaleAVAX(1_000_000),
						MinBalance: scaleAVAX(1_000_000),
					},
				},
			},
			AtomicRequests: &chainsatomic.Requests{
				PutRequests: []*chainsatomic.Element{},
			},
			InputIDs: set.Of(
				ids.ID(common.FromHex("0x0000000000000005000000140100000000000000000000000000000000000000")),
				ids.ID(common.FromHex("0x0000000000000007000000140200000000000000000000000000000000000000")),
			),
		},
		{
			Name: "import_non_avax", // Synthetic
			Old: &atomic.Tx{
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
			New: &Tx{
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
			JSON: `{
				"unsignedTx":{
					"networkID":0,
					"blockchainID":"11111111111111111111111111111111LpoYY",
					"sourceChain":"11111111111111111111111111111111LpoYY",
					"importedInputs":[{
						"txID":"11111111111111111111111111111111LpoYY",
						"outputIndex":0,
						"assetID":"11111111111111111111111111111111LpoYY",
						"fxID":"11111111111111111111111111111111LpoYY",
						"input":{"amount":999,"signatureIndices":[]}
					}],
					"outputs":[{
						"address":"0x0000000000000000000000000000000000000000",
						"amount":999,
						"assetID":"11111111111111111111111111111111LpoYY"
					}]
				},
				"credentials":[]
			}`,
			Bytes: common.FromHex("0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000500000000000003e70000000000000001000000000000000000000000000000000000000000000000000003e7000000000000000000000000000000000000000000000000000000000000000000000000"),
			Op: hook.Op{
				ID:   ids.FromStringOrPanic("s4xoHkf4rPQYSwjbQo78hcSP1wSeViV1Fx2PHM4AfRiDurFkf"),
				Gas:  10226,
				Mint: map[common.Address]uint256.Int{},
			},
			AtomicRequests: &chainsatomic.Requests{
				RemoveRequests: [][]byte{
					common.FromHex("0x2c34ce1df23b838c5abf2a7f6437cca3d3067ed509ff25f11df6b11b582b51eb"),
				},
			},
			InputIDs: set.Of(
				ids.ID(common.FromHex("0x2c34ce1df23b838c5abf2a7f6437cca3d3067ed509ff25f11df6b11b582b51eb")),
			),
		},
	}
	OldTxs []*atomic.Tx
	NewTxs []*Tx
)

func init() {
	OldTxs = make([]*atomic.Tx, len(Tests))
	NewTxs = make([]*Tx, len(Tests))
	for i, test := range Tests {
		OldTxs[i] = test.Old
		NewTxs[i] = test.New
	}
}

// MainnetContext returns a [snow.Context] with mainnet values.
func MainnetContext() *snow.Context {
	return &snow.Context{
		NetworkID:   constants.MainnetID,
		SubnetID:    constants.PrimaryNetworkID,
		ChainID:     CChainID,
		XChainID:    XChainID,
		CChainID:    CChainID,
		AVAXAssetID: AVAXAssetID,
		ValidatorState: &validatorstest.State{
			GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
				switch chainID {
				case constants.PlatformChainID, XChainID, CChainID:
					return constants.PrimaryNetworkID, nil
				default:
					return ids.GenerateTestID(), nil
				}
			},
		},
	}
}

func TestID(t *testing.T) {
	for _, test := range Tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				// We must parse the old tx to properly initialize the ID.
				old, err := ParseOldTx(test.Bytes)
				require.NoError(t, err, "ParseOldTx()")
				assert.Equalf(t, test.Op.ID, old.ID(), "%T.ID()", old)
			})
			t.Run("new", func(t *testing.T) {
				assert.Equalf(t, test.Op.ID, test.New.ID(), "%T.ID()", test.New)
			})
		})
	}
}

func TestBytes(t *testing.T) {
	for _, test := range Tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := atomic.Codec.Marshal(atomic.CodecVersion, test.Old)
				require.NoErrorf(t, err, "%T.Marshal(, %T)", atomic.Codec, test.Old)
				assert.Equalf(t, test.Bytes, got, "%T.Marshal(, %T)", atomic.Codec, test.Old)
			})
			t.Run("new", func(t *testing.T) {
				got, err := test.New.Bytes()
				require.NoErrorf(t, err, "%T.Bytes()", test.New)
				assert.Equalf(t, test.Bytes, got, "%T.Bytes()", test.New)
			})
		})
	}
}

func TestMarshalSlice(t *testing.T) {
	want, err := atomic.Codec.Marshal(atomic.CodecVersion, OldTxs)
	require.NoErrorf(t, err, "%T.Marshal(, %T)", atomic.Codec, OldTxs)

	tests := []struct {
		name string
		txs  []*Tx
		want []byte
	}{
		{
			name: "mainnet",
			txs:  NewTxs,
			want: want,
		},
		{
			name: "empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MarshalSlice(test.txs)
			require.NoErrorf(t, err, "MarshalSlice(%T)", test.txs)
			assert.Equalf(t, test.want, got, "MarshalSlice(%T)", test.txs)
		})
	}
}

// OldCmpOpt returns a configuration for [cmp.Diff] to compare [atomic.Tx]
// instances.
func OldCmpOpt() cmp.Option {
	return cmputils.IfIn[atomic.Tx](cmp.Options{
		cmpopts.IgnoreUnexported(
			atomic.Metadata{},
			avax.UTXOID{},
			secp256k1fx.OutputOwners{},
		),
		cmpopts.EquateEmpty(),
	})
}

// CmpOpt returns a configuration for [cmp.Diff] to compare [Tx] instances.
func CmpOpt() cmp.Option {
	return cmputils.IfIn[Tx](cmp.Options{
		cmpopts.IgnoreUnexported(
			avax.UTXOID{},
			secp256k1fx.OutputOwners{},
		),
		cmpopts.EquateEmpty(),
	})
}

func TestParse(t *testing.T) {
	for _, test := range Tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := ParseOldTx(test.Bytes)
				require.NoError(t, err, "ParseOldTx()")
				if diff := cmp.Diff(test.Old, got, OldCmpOpt()); diff != "" {
					t.Errorf("%T.Unmarshal(, %T) diff (-want +got):\n%s", atomic.Codec, got, diff)
				}
			})
			t.Run("new", func(t *testing.T) {
				got, err := Parse(test.Bytes)
				require.NoError(t, err, "Parse()")
				if diff := cmp.Diff(test.New, got, CmpOpt()); diff != "" {
					t.Errorf("Parse() diff (-want +got):\n%s", diff)
				}
			})
		})
	}
}

func TestParseSlice(t *testing.T) {
	bytes, err := atomic.Codec.Marshal(atomic.CodecVersion, OldTxs)
	require.NoErrorf(t, err, "%T.Marshal(, %T)", atomic.Codec, OldTxs)

	tests := []struct {
		name    string
		bytes   []byte
		want    []*Tx
		wantErr error
	}{
		{
			name:  "mainnet",
			bytes: bytes,
			want:  NewTxs,
		},
		{
			name: "empty",
		},
		{
			name: "inefficient",
			bytes: []byte{
				// codecVersion:
				0x00, 0x00,
				// len(txs):
				0x00, 0x00, 0x00, 0x00,
			},
			wantErr: errInefficientSlicePacking,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseSlice(test.bytes)
			require.ErrorIs(t, err, test.wantErr, "ParseSlice()")
			if diff := cmp.Diff(test.want, got, CmpOpt()); diff != "" {
				t.Errorf("ParseSlice() diff (-want +got):\n%s", diff)
			}
		})
	}
}

// ToOldTx converts a transaction from the new format into coreth's old format.
func ToOldTx(tb testing.TB, newTx *Tx) *atomic.Tx {
	tb.Helper()

	bytes, err := newTx.Bytes()
	require.NoErrorf(tb, err, "%T.Bytes()", newTx)

	oldTx, err := ParseOldTx(bytes)
	require.NoError(tb, err, "ParseOldTx()")
	return oldTx
}

var errUnexpectedCredentialType = errors.New("unexpected credential type")

// ParseOldTx parses a transaction using coreth's old parsing logic but enforces
// additional restrictions. Coreth's parsing logic is overly permissive and
// depends on later verification in [vm.VerifierBackend].
func ParseOldTx(b []byte) (*atomic.Tx, error) {
	tx, err := atomic.ExtractAtomicTx(b, atomic.Codec)
	if err != nil {
		return nil, err
	}
	for _, cred := range tx.Creds {
		if _, ok := cred.(*secp256k1fx.Credential); !ok {
			return nil, errUnexpectedCredentialType
		}
	}
	return tx, nil
}

// ParseOldTxs parses a slice of transactions using coreth's old parsing logic
// but enforces additional restrictions. Coreth's parsing logic is overly
// permissive and depends on later verification in [vm.VerifierBackend].
func ParseOldTxs(b []byte) ([]*atomic.Tx, error) {
	txs, err := atomic.ExtractAtomicTxs(b, true, atomic.Codec)
	if err != nil {
		return nil, err
	}
	for _, tx := range txs {
		for _, cred := range tx.Creds {
			if _, ok := cred.(*secp256k1fx.Credential); !ok {
				return nil, errUnexpectedCredentialType
			}
		}
	}
	return txs, nil
}

func FuzzParseCompatibility(f *testing.F) {
	for _, test := range Tests {
		f.Add(test.Bytes)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, oldErr := ParseOldTx(data)
		oldOk := oldErr == nil

		_, newErr := Parse(data)
		newOk := newErr == nil

		assert.Equal(t, oldOk, newOk, "Parse(b) == ParseOldTx(b)")
	})
}

func FuzzParseSliceCompatibility(f *testing.F) {
	{
		b, err := MarshalSlice(NewTxs)
		require.NoError(f, err, "MarshalSlice()")
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_, oldErr := ParseOldTxs(data)
		oldOk := oldErr == nil

		_, newErr := ParseSlice(data)
		newOk := newErr == nil

		assert.Equal(t, oldOk, newOk, "ParseSlice(b) == ParseOldTxs(b)")
	})
}

func FuzzParseSliceRoundTrip(f *testing.F) {
	{
		b, err := MarshalSlice(NewTxs)
		require.NoError(f, err, "MarshalSlice()")
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		txs, err := ParseSlice(data)
		if err != nil {
			return
		}

		got, err := MarshalSlice(txs)
		require.NoError(t, err, "MarshalSlice()")
		if diff := cmp.Diff(data, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("MarshalSlice(ParseSlice()) diff (-want +got):\n%s", diff)
		}
	})
}

func TestJSONMarshal(t *testing.T) {
	for _, test := range Tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := json.Marshal(test.Old)
				require.NoErrorf(t, err, "json.Marshal(%T)", test.Old)
				assert.JSONEqf(t, test.JSON, string(got), "json.Marshal(%T)", test.Old)
			})
			t.Run("new", func(t *testing.T) {
				got, err := json.Marshal(test.New)
				require.NoErrorf(t, err, "json.Marshal(%T)", test.New)
				assert.JSONEqf(t, test.JSON, string(got), "json.Marshal(%T)", test.New)
			})
		})
	}
}

func TestAsOp(t *testing.T) {
	for _, test := range Tests {
		t.Run(test.Name, func(t *testing.T) {
			got, err := test.New.AsOp(AVAXAssetID)
			require.NoErrorf(t, err, "%T.AsOp(AVAXAssetID)", test.New)
			assert.Equalf(t, test.Op, got, "%T.AsOp(AVAXAssetID)", test.New)
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
			want: errMultipleNonces,
		},
		{
			name: "import_burned_overflow",
			tx: &Import{
				ImportedInputs: []*avax.TransferableInput{
					{
						Asset: avax.Asset{ID: AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: math.MaxUint64,
						},
					},
					{
						Asset: avax.Asset{ID: AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 2,
						},
					},
				},
				Outs: []Output{{
					AssetID: AVAXAssetID,
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
						Asset: avax.Asset{ID: AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: math.MaxUint64,
						},
					},
					{
						Asset: avax.Asset{ID: AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt: 1,
						},
					},
				},
				Outs: []Output{{
					AssetID: AVAXAssetID,
					Amount:  1,
				}},
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "import_burned_underflow",
			tx: &Import{
				ImportedInputs: []*avax.TransferableInput{{
					Asset: avax.Asset{ID: AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				}},
				Outs: []Output{{
					AssetID: AVAXAssetID,
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
						AssetID: AVAXAssetID,
						Amount:  math.MaxUint64,
					},
					{
						Address: common.Address{1},
						AssetID: AVAXAssetID,
						Amount:  2,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: AVAXAssetID},
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
						AssetID: AVAXAssetID,
						Amount:  math.MaxUint64,
					},
					{
						Address: common.Address{1},
						AssetID: AVAXAssetID,
						Amount:  1,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: AVAXAssetID},
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
					AssetID: AVAXAssetID,
					Amount:  1,
				}},
				ExportedOutputs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: AVAXAssetID},
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
			_, err := tx.AsOp(AVAXAssetID)
			require.ErrorIsf(t, err, test.want, "%T.AsOp(AVAXAssetID)", tx)
		})
	}
}

func TestAtomicRequests(t *testing.T) {
	for _, test := range Tests {
		t.Run(test.Name, func(t *testing.T) {
			chainID, requests, err := test.New.AtomicRequests()
			require.NoErrorf(t, err, "%T.AtomicRequests()", test.New)
			assert.Equalf(t, test.AtomicRequestsChainID, chainID, "%T.AtomicRequests().ChainID", test.New)
			assert.Equalf(t, test.AtomicRequests, requests, "%T.AtomicRequests().Requests", test.New)
		})
	}
}

func TestInputIDs(t *testing.T) {
	for _, test := range Tests {
		t.Run(test.Name, func(t *testing.T) {
			got := test.New.InputIDs()
			assert.Equalf(t, test.InputIDs, got, "%T.InputIDs()", test.New)
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

func NewEmptyStateDB(t testing.TB) *extstate.StateDB {
	t.Helper()

	db := state.NewDatabase(rawdb.NewMemoryDatabase())
	sdb, err := state.New(types.EmptyRootHash, db, nil)
	require.NoError(t, err)
	return extstate.New(sdb)
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
					{Address: alice, Amount: 1, AssetID: AVAXAssetID},
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
			name: "import_many",
			tx: &Import{
				Outs: []Output{
					{Address: alice, Amount: 1, AssetID: AVAXAssetID},
					{Address: alice, Amount: 10, AssetID: AVAXAssetID},
					{Address: bob, Amount: 100, AssetID: AVAXAssetID},
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
					{Address: alice, Amount: 1, AssetID: AVAXAssetID},
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
					{Address: alice, Amount: 1, AssetID: AVAXAssetID},
					{Address: alice, Amount: 10, AssetID: AVAXAssetID},
					{Address: bob, Amount: 100, AssetID: AVAXAssetID},
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
			wantErr: errInsufficientFunds,
		},
		{
			name: "export_non_avax_total_insufficient",
			init: map[common.Address]map[ids.ID]uint64{
				alice: {
					btc: 1,
				},
			},
			tx: &Export{
				Ins: []Input{
					{Address: alice, Amount: 1, AssetID: btc},
					{Address: alice, Amount: 1, AssetID: btc},
				},
			},
			wantErr: errInsufficientFunds,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				state = NewEmptyStateDB(t)
				toBig = func(v uint64) *big.Int { return new(big.Int).SetUint64(v) }
			)
			for addr, balances := range test.init {
				for assetID, amount := range balances {
					coinID := common.Hash(assetID)
					state.AddBalanceMultiCoin(addr, coinID, toBig(amount))
				}
			}

			err := test.tx.TransferNonAVAX(AVAXAssetID, state)
			require.ErrorIs(t, err, test.wantErr)
			for _, addr := range []common.Address{alice, bob} {
				for _, asset := range []ids.ID{AVAXAssetID, btc, eth} {
					want := toBig(test.want[addr][asset])
					coinID := common.Hash(asset)
					got := state.GetBalanceMultiCoin(addr, coinID)
					if diff := cmp.Diff(want, got, cmputils.BigInts()); diff != "" {
						t.Errorf("%T.GetBalanceMultiCoin(%s, %s) diff (-want +got):\n%s", state, addr, coinID, diff)
					}
				}
			}
		})
	}
}

// OldSanityCheck behaves like [Tx.SanityCheck] for the legacy [atomic.Tx].
func OldSanityCheck(tx *atomic.Tx, ctx *snow.Context) error {
	rules := *extrastest.ForkToRules(upgradetest.Helicon)
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

func TestSanityCheck(t *testing.T) {
	var (
		ctx     = MainnetContext()
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
			name: "import_mainnet",
			tx:   Tests[0].New.Unsigned,
		},
		{
			name:    "import_wrong_network_id",
			tx:      imp(func(i *Import) { i.NetworkID++ }),
			wantErr: errWrongNetworkID,
		},
		{
			name:    "import_wrong_chain_id",
			tx:      imp(func(i *Import) { i.BlockchainID = XChainID }),
			wantErr: errWrongChainID,
		},
		{
			name:    "import_wrong_source_chain",
			tx:      imp(func(i *Import) { i.SourceChain = CChainID }),
			wantErr: errNotSameSubnet,
		},
		{
			name:    "import_no_inputs",
			tx:      imp(func(i *Import) { i.ImportedInputs = nil }),
			wantErr: errNoInputs,
		},
		{
			name:    "import_no_outputs",
			tx:      imp(func(i *Import) { i.Outs = nil }),
			wantErr: errNoOutputs,
		},
		{
			name:    "import_invalid_input",
			tx:      imp(func(i *Import) { i.ImportedInputs[0].In.(*secp256k1fx.TransferInput).Amt = 0 }),
			wantErr: errInvalidInput,
		},
		{
			name:    "import_non_avax_input",
			tx:      imp(func(i *Import) { i.ImportedInputs[0].Asset.ID = nonAVAX }),
			wantErr: errNonAVAXInput,
		},
		{
			name:    "import_zero_amount_output",
			tx:      imp(func(i *Import) { i.Outs[0].Amount = 0 }),
			wantErr: errInvalidOutput,
		},
		{
			name:    "import_non_avax_output",
			tx:      imp(func(i *Import) { i.Outs[0].AssetID = nonAVAX }),
			wantErr: errNonAVAXOutput,
		},
		{
			name:    "import_flow_check_failed",
			tx:      imp(func(i *Import) { i.Outs[0].Amount = 200 }),
			wantErr: errFlowCheckFailed,
		},
		{
			name: "import_inputs_not_sorted_unique",
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
			wantErr: errInputsNotSortedUnique,
		},
		{
			name: "import_outputs_not_sorted_unique",
			tx: imp(func(i *Import) {
				i.Outs = []Output{
					{Amount: 50, AssetID: ctx.AVAXAssetID},
					{Amount: 50, AssetID: ctx.AVAXAssetID},
				}
			}),
			wantErr: errOutputsNotSortedUnique,
		},
		{
			name: "export_valid",
			tx:   validExport(),
		},
		{
			name: "export_mainnet",
			tx:   Tests[1].New.Unsigned,
		},
		{
			name:    "export_wrong_network_id",
			tx:      exp(func(e *Export) { e.NetworkID++ }),
			wantErr: errWrongNetworkID,
		},
		{
			name:    "export_wrong_chain_id",
			tx:      exp(func(e *Export) { e.BlockchainID = XChainID }),
			wantErr: errWrongChainID,
		},
		{
			name:    "export_wrong_destination_chain",
			tx:      exp(func(e *Export) { e.DestinationChain = CChainID }),
			wantErr: errNotSameSubnet,
		},
		{
			name:    "export_no_inputs",
			tx:      exp(func(e *Export) { e.Ins = nil }),
			wantErr: errNoInputs,
		},
		{
			name:    "export_no_outputs",
			tx:      exp(func(e *Export) { e.ExportedOutputs = nil }),
			wantErr: errNoOutputs,
		},
		{
			name:    "export_zero_amount_input",
			tx:      exp(func(e *Export) { e.Ins[0].Amount = 0 }),
			wantErr: errInvalidInput,
		},
		{
			name:    "export_non_avax_input",
			tx:      exp(func(e *Export) { e.Ins[0].AssetID = nonAVAX }),
			wantErr: errNonAVAXInput,
		},
		{
			name:    "export_invalid_output",
			tx:      exp(func(e *Export) { e.ExportedOutputs[0].Out.(*secp256k1fx.TransferOutput).Amt = 0 }),
			wantErr: errInvalidOutput,
		},
		{
			name:    "export_non_avax_output",
			tx:      exp(func(e *Export) { e.ExportedOutputs[0].Asset.ID = nonAVAX }),
			wantErr: errNonAVAXOutput,
		},
		{
			name:    "export_flow_check_failed",
			tx:      exp(func(e *Export) { e.ExportedOutputs[0].Out.(*secp256k1fx.TransferOutput).Amt = 200 }),
			wantErr: errFlowCheckFailed,
		},
		{
			name: "export_inputs_not_sorted_unique",
			tx: exp(func(e *Export) {
				e.Ins = []Input{
					{Amount: 50, AssetID: ctx.AVAXAssetID},
					{Amount: 50, AssetID: ctx.AVAXAssetID},
				}
			}),
			wantErr: errInputsNotSortedUnique,
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
			wantErr: errOutputsNotSorted,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.tx.SanityCheck(ctx)
			require.ErrorIsf(t, err, test.wantErr, "%T.SanityCheck()", test.tx)
		})
	}
}

func TestVerifyCredentials(t *testing.T) {
	// Signatures are hardcoded: secp256k1 uses RFC 6979 deterministic nonces, so
	// signing the same canonical bytes with the same key always produces the
	// same output. To regenerate after changing a valid* helper below, sign
	// [SignedBytes] of the produced Unsigned with the corresponding key.
	verifyKey, err := secp256k1.ToPrivateKey(common.FromHex("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"))
	require.NoError(t, err)
	verifyOther, err := secp256k1.ToPrivateKey(common.FromHex("0x9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f1234"))
	require.NoError(t, err)
	var (
		verifyKeyEth   = verifyKey.EthAddress()
		verifyOtherEth = verifyOther.EthAddress()
		verifyKeyShort = verifyKey.Address()

		// validImportTx returns a freshly-allocated import Tx with one
		// AVAX-denominated input authorized by verifyKey, signed by verifyKey.
		validImportTx = func() *Tx {
			return &Tx{
				Unsigned: &Import{
					SourceChain: XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{TxID: ids.ID{1}},
						Asset:  avax.Asset{ID: AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   100,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
				},
				Creds: []Credential{&secp256k1fx.Credential{Sigs: [][65]byte{
					[65]byte(common.FromHex("0x393d75eaa2d49672b14b5445aa6c357cc894e47172c5f80c6ae135991496722a07a76b05832ec365b37bdccbfcb589e2c217383ce4660cc1a61a45421b5623ec00")),
				}}},
			}
		}
		// validImportUTXOs returns the freshly-allocated shared-memory element
		// that authorizes [validImportTx]'s input.
		validImportUTXOs = func() []*chainsatomic.Element {
			b, err := c.Marshal(codecVersion, &avax.UTXO{
				UTXOID: avax.UTXOID{TxID: ids.ID{1}},
				Asset:  avax.Asset{ID: AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 100,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{verifyKeyShort},
					},
				},
			})
			require.NoError(t, err)
			key := (&avax.UTXOID{TxID: ids.ID{1}}).InputID()
			return []*chainsatomic.Element{{
				Key:   key[:],
				Value: b,
			}}
		}

		// validExportTx returns a freshly-allocated export Tx with one input
		// owned by verifyKey, signed by verifyKey. It needs no shared memory.
		validExportTx = func() *Tx {
			return &Tx{
				Unsigned: &Export{
					Ins: []Input{{Address: verifyKeyEth, Amount: 100, AssetID: AVAXAssetID}},
				},
				Creds: []Credential{&secp256k1fx.Credential{Sigs: [][65]byte{
					[65]byte(common.FromHex("0xfe088f91a447b6ebd89a18405e07a50869eaeee869ab924a0ec1174e94d293764e56260eca1d6d26a77f33aa8a99f4b4917f7598bc680b87b01f6c9a750e293d00")),
				}}},
			}
		}

		// imp / exp build fresh valid txs and apply a mutation in place.
		imp = func(mutate func(*Tx)) *Tx {
			tx := validImportTx()
			mutate(tx)
			return tx
		}
		exp = func(mutate func(*Tx)) *Tx {
			tx := validExportTx()
			mutate(tx)
			return tx
		}
		// utxos applies a mutation to a fresh copy of [validImportUTXOs].
		utxos = func(mutate func([]*chainsatomic.Element)) []*chainsatomic.Element {
			u := validImportUTXOs()
			mutate(u)
			return u
		}
	)

	tests := []struct {
		name string
		tx   *Tx
		// utxos are seeded under XChainID → CChainID, keyed by Element.Key.
		utxos   []*chainsatomic.Element
		wantErr error
	}{
		{
			name:  "import_valid",
			tx:    validImportTx(),
			utxos: validImportUTXOs(),
		},
		{
			name:    "import_wrong_num_credentials",
			tx:      imp(func(tx *Tx) { tx.Creds = nil }),
			utxos:   validImportUTXOs(),
			wantErr: errIncorrectNumCredentials,
		},
		{
			name:    "import_missing_utxo",
			tx:      validImportTx(),
			wantErr: errFetchingUTXOs,
		},
		{
			name: "import_unmarshalling_utxo",
			tx:   validImportTx(),
			utxos: utxos(func(u []*chainsatomic.Element) {
				u[0].Value = []byte{0xff, 0xff, 0xff}
			}),
			wantErr: errUnmarshallingUTXO,
		},
		{
			// Input claims a non-AVAX asset, but the UTXO at that key has AVAX.
			name: "import_mismatched_asset_ids",
			tx: imp(func(tx *Tx) {
				tx.Unsigned.(*Import).ImportedInputs[0].Asset.ID = ids.ID{0xab}
			}),
			utxos:   validImportUTXOs(),
			wantErr: errMismatchedAssetIDs,
		},
		{
			// Signed by verifyOther; UTXO is owned by verifyKey.
			name: "import_wrong_signer",
			tx: imp(func(tx *Tx) {
				tx.Creds = []Credential{&secp256k1fx.Credential{Sigs: [][65]byte{
					[65]byte(common.FromHex("0x3b42ba9f4653f9181090b64cda757f13bf471f4d89911f4c71a107931c8f5af6082282ed6e9d2969873142a5e3675b7169bce5cf28438730d04e7432b7349e8801")),
				}}}
			}),
			utxos:   validImportUTXOs(),
			wantErr: errVerifyingTransfer,
		},
		{
			name: "export_valid",
			tx:   validExportTx(),
		},
		{
			name:    "export_wrong_num_credentials",
			tx:      exp(func(tx *Tx) { tx.Creds = nil }),
			wantErr: errIncorrectNumCredentials,
		},
		{
			name: "export_zero_signatures",
			tx: exp(func(tx *Tx) {
				tx.Creds = []Credential{&secp256k1fx.Credential{Sigs: nil}}
			}),
			wantErr: errIncorrectNumSignatures,
		},
		{
			name: "export_two_signatures",
			tx: exp(func(tx *Tx) {
				tx.Creds = []Credential{&secp256k1fx.Credential{Sigs: [][65]byte{{}, {}}}}
			}),
			wantErr: errIncorrectNumSignatures,
		},
		{
			// 65 zero bytes is structurally well-formed but unrecoverable.
			name: "export_invalid_signature",
			tx: exp(func(tx *Tx) {
				tx.Creds = []Credential{&secp256k1fx.Credential{Sigs: [][65]byte{{}}}}
			}),
			wantErr: secp256k1.ErrInvalidSig,
		},
		{
			// Signature is from verifyKey, but Input.Address is verifyOther's.
			name: "export_address_mismatch",
			tx: exp(func(tx *Tx) {
				tx.Unsigned.(*Export).Ins[0].Address = verifyOtherEth
			}),
			wantErr: errAddressMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			memory := chainsatomic.NewMemory(memdb.New())
			xMemory := memory.NewSharedMemory(XChainID)
			require.NoError(t, xMemory.Apply(map[ids.ID]*chainsatomic.Requests{
				CChainID: {PutRequests: test.utxos},
			}))

			cMemory := memory.NewSharedMemory(CChainID)
			err := test.tx.VerifyCredentials(cMemory)
			assert.ErrorIsf(t, err, test.wantErr, "%T.VerifyCredentials()", test.tx)
		})
	}
}
