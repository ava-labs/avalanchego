// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx_test

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"

	. "github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

var goldens = [...]goldenTx{
	importTx,
	exportTx,
	importMultiInputTx,
	exportSameAddressMultiAssetTx,
	exportMultiAddressMultiAssetTx,
	importNonAVAXTx,
}

func TestID(t *testing.T) {
	for _, golden := range goldens {
		t.Run(golden.name, func(t *testing.T) {
			assert.Equalf(t, golden.id, golden.tx.ID(), "%T.ID()", golden.tx)
		})
	}
}

func TestBytes(t *testing.T) {
	for _, golden := range goldens {
		t.Run(golden.name, func(t *testing.T) {
			got, err := golden.tx.Bytes()
			require.NoErrorf(t, err, "%T.Bytes()", golden.tx)
			assert.Equalf(t, golden.bytes, got, "%T.Bytes()", golden.tx)
		})
	}
}

func TestParse(t *testing.T) {
	for _, golden := range goldens {
		t.Run(golden.name, func(t *testing.T) {
			got, err := Parse(golden.bytes)
			require.NoError(t, err, "Parse()")
			if diff := cmp.Diff(golden.tx, got, txtest.CmpOpt()); diff != "" {
				t.Errorf("Parse() diff (-want +got):\n%s", diff)
			}
		})
	}
}

// fuzz seeds f with [goldens], specifies simple alphabets used to bias the
// fuzzer, and fuzzes the test.
func fuzz(f *testing.F, ff func(t *testing.T, tx *Tx)) {
	fuzzer := &txtest.F{
		F: f,
		Addresses: []common.Address{
			{1},
		},
		AssetIDs: []ids.ID{
			avaxAssetID,
		},
	}
	for _, golden := range goldens {
		fuzzer.Add(golden.tx)
	}
	fuzzer.Fuzz(ff)
}

func FuzzParseRoundTrip(f *testing.F) {
	fuzz(f, func(t *testing.T, want *Tx) {
		bytes, err := want.Bytes()
		require.NoErrorf(t, err, "%T.Bytes()", want)

		got, err := Parse(bytes)
		require.NoError(t, err, "Parse()")
		if diff := cmp.Diff(want, got, txtest.CmpOpt()); diff != "" {
			t.Errorf("Parse() diff (-want +got):\n%s", diff)
		}
	})
}

// goldensSlice returns [goldens] along with their expected encoding as a slice.
func goldensSlice() ([]*Tx, []byte) {
	const codecVersionLen = 2
	var (
		txs   = make([]*Tx, len(goldens))
		bytes = make([]byte, codecVersionLen, 64)
	)
	bytes = binary.BigEndian.AppendUint32(bytes, uint32(len(goldens)))
	for i, golden := range goldens {
		txs[i] = golden.tx
		bytes = append(bytes, golden.bytes[codecVersionLen:]...)
	}
	return txs, bytes
}

func TestMarshalSlice(t *testing.T) {
	txs, want := goldensSlice()

	tests := []struct {
		name string
		txs  []*Tx
		want []byte
	}{
		{
			name: "mainnet",
			txs:  txs,
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

func TestParseSlice(t *testing.T) {
	txs, bytes := goldensSlice()

	tests := []struct {
		name    string
		bytes   []byte
		want    []*Tx
		wantErr error
	}{
		{
			name:  "mainnet",
			bytes: bytes,
			want:  txs,
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
			wantErr: ErrInefficientSlicePacking,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseSlice(test.bytes)
			require.ErrorIs(t, err, test.wantErr, "ParseSlice()")
			if diff := cmp.Diff(test.want, got, txtest.CmpOpt()); diff != "" {
				t.Errorf("ParseSlice() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFromBlock(t *testing.T) {
	txs := make([]*Tx, len(goldens))
	for i, golden := range goldens {
		txs[i] = golden.tx
	}

	sliceBytes, err := MarshalSlice(txs)
	require.NoError(t, err, "MarshalSlice()")

	const (
		preAP5Time uint64 = 0
		ap5Time           = preAP5Time + 1
	)
	config := corethparams.WithExtra(
		&params.ChainConfig{},
		&extras.ChainConfig{
			NetworkUpgrades: extras.NetworkUpgrades{
				ApricotPhase5BlockTimestamp: new(ap5Time),
			},
		},
	)

	tests := []struct {
		name    string
		time    uint64
		extData []byte
		want    []*Tx
		wantErr error
	}{
		{
			name: "pre_ap5_empty",
			time: preAP5Time,
		},
		{
			name: "pre_ap5_empty_slice",
			time: preAP5Time,
			extData: []byte{
				// codecVersion:
				0x00, 0x00,
				// len(txs):
				0x00, 0x00, 0x00, 0x00,
			},
			wantErr: wrappers.ErrInsufficientLength,
		},
		{
			name:    "pre_ap5_single",
			time:    preAP5Time,
			extData: importTx.bytes,
			want:    []*Tx{importTx.tx},
		},
		{
			name: "ap5_empty",
			time: ap5Time,
		},
		{
			name: "ap5_empty_slice",
			time: ap5Time,
			extData: []byte{
				// codecVersion:
				0x00, 0x00,
				// len(txs):
				0x00, 0x00, 0x00, 0x00,
			},
			wantErr: ErrInefficientSlicePacking,
		},
		{
			name:    "ap5_single",
			time:    ap5Time,
			extData: importTx.bytes,
			wantErr: codec.ErrExtraSpace,
		},
		{
			name:    "ap5_slice",
			time:    ap5Time,
			extData: sliceBytes,
			want:    txs,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			block := customtypes.NewBlockWithExtData(
				&types.Header{
					Time: test.time,
				},
				nil, // txs
				nil, // uncles
				nil, // receipts
				nil, // hasher, unused without txs
				test.extData,
				true, // update [customtypes.HeaderExtra.ExtDataHash]
			)
			got, err := FromBlock(config, block)
			require.ErrorIs(t, err, test.wantErr, "FromBlock()")
			if diff := cmp.Diff(test.want, got, txtest.CmpOpt()); diff != "" {
				t.Errorf("FromBlock() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func FuzzParseSliceRoundTrip(f *testing.F) {
	{
		txs := make([]*Tx, len(goldens))
		for i, golden := range goldens {
			txs[i] = golden.tx
		}
		b, err := MarshalSlice(txs)
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
	tests := []struct {
		golden goldenTx
		want   string
	}{
		{
			golden: importTx,
			want: `{
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
		},
		{
			golden: exportTx,
			want: `{
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
		},
		{
			golden: importMultiInputTx,
			want: `{
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
		},
		{
			golden: exportSameAddressMultiAssetTx,
			want: `{
				"unsignedTx":{
					"networkID":0,
					"blockchainID":"11111111111111111111111111111111LpoYY",
					"destinationChain":"11111111111111111111111111111111LpoYY",
					"inputs":[
						{"address":"0x0000000000000000000000000000000000000000","amount":999,"assetID":"11111111111111111111111111111111LpoYY","nonce":5},
						{"address":"0x0000000000000000000000000000000000000000","amount":1000000,"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z","nonce":5}
					],
					"exportedOutputs":[
						{
							"assetID":"11111111111111111111111111111111LpoYY",
							"fxID":"11111111111111111111111111111111LpoYY",
							"output":{
								"addresses":["GVsscSys19nXbNEJi5g1Z1y8UawXee8gj"],
								"amount":100,
								"locktime":0,
								"threshold":1
							}
						},
						{
							"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
							"fxID":"11111111111111111111111111111111LpoYY",
							"output":{
								"addresses":["GVsscSys19nXbNEJi5g1Z1y8UawXee8gj"],
								"amount":100000,
								"locktime":0,
								"threshold":1
							}
						}
					]
				},
				"credentials":[]
			}`,
		},
		{
			golden: exportMultiAddressMultiAssetTx,
			want: `{
				"unsignedTx":{
					"networkID":0,
					"blockchainID":"11111111111111111111111111111111LpoYY",
					"destinationChain":"11111111111111111111111111111111LpoYY",
					"inputs":[
						{"address":"0x0100000000000000000000000000000000000000","amount":999,"assetID":"11111111111111111111111111111111LpoYY","nonce":5},
						{"address":"0x0200000000000000000000000000000000000000","amount":1000000,"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z","nonce":7}
					],
					"exportedOutputs":[
						{
							"assetID":"11111111111111111111111111111111LpoYY",
							"fxID":"11111111111111111111111111111111LpoYY",
							"output":{
								"addresses":["J3mMsbNx1AfUrQMSHBwWcDfYRYY1i7rGE","Kber8jn31BYS7SUZrJD1fRMxNW8MvZnhY"],
								"amount":500,
								"locktime":0,
								"threshold":2
							}
						},
						{
							"assetID":"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
							"fxID":"11111111111111111111111111111111LpoYY",
							"output":{
								"addresses":["J3mMsbNx1AfUrQMSHBwWcDfYRYY1i7rGE","Kber8jn31BYS7SUZrJD1fRMxNW8MvZnhY"],
								"amount":500000,
								"locktime":0,
								"threshold":2
							}
						}
					]
				},
				"credentials":[]
			}`,
		},
		{
			golden: importNonAVAXTx,
			want: `{
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
		},
	}
	for _, test := range tests {
		t.Run(test.golden.name, func(t *testing.T) {
			tx := test.golden.tx
			got, err := json.Marshal(tx)
			require.NoErrorf(t, err, "json.Marshal(%T)", tx)
			assert.JSONEqf(t, test.want, string(got), "json.Marshal(%T)", tx)
		})
	}
}
