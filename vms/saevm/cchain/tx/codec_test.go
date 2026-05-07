// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Imported for [vm.VerifierBackend] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	. "github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

var allGolden = [...]golden{
	importGolden,
	exportGolden,
	importMultiInputGolden,
	exportSameAddressMultiAssetGolden,
	exportMultiAddressMultiAssetGolden,
	importNonAVAXGolden,
}

func TestID(t *testing.T) {
	for _, tx := range allGolden {
		t.Run(tx.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				// We must parse the old tx to properly initialize the ID.
				old, err := parseOldTx(tx.bytes)
				require.NoError(t, err, "parseOldTx()")
				assert.Equalf(t, tx.id, old.ID(), "%T.ID()", old)
			})
			t.Run("new", func(t *testing.T) {
				assert.Equalf(t, tx.id, tx.new.ID(), "%T.ID()", tx.new)
			})
		})
	}
}

func TestBytes(t *testing.T) {
	for _, tx := range allGolden {
		t.Run(tx.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := atomic.Codec.Marshal(atomic.CodecVersion, tx.old)
				require.NoErrorf(t, err, "%T.Marshal(, %T)", atomic.Codec, tx.old)
				assert.Equalf(t, tx.bytes, got, "%T.Marshal(, %T)", atomic.Codec, tx.old)
			})
			t.Run("new", func(t *testing.T) {
				got, err := tx.new.Bytes()
				require.NoErrorf(t, err, "%T.Bytes()", tx.new)
				assert.Equalf(t, tx.bytes, got, "%T.Bytes()", tx.new)
			})
		})
	}
}

var errUnexpectedCredentialType = errors.New("unexpected credential type")

// parseOldTx parses a transaction using coreth's old parsing logic but enforces
// additional restrictions. Coreth's parsing logic is overly permissive and
// depends on later verification in [vm.VerifierBackend].
func parseOldTx(b []byte) (*atomic.Tx, error) {
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

// oldCmpOpt returns a configuration for [cmp.Diff] to compare [atomic.Tx]
// instances.
func oldCmpOpt() cmp.Option {
	return cmputils.IfIn[atomic.Tx](cmp.Options{
		cmpopts.IgnoreUnexported(
			atomic.Metadata{},
			avax.UTXOID{},
			secp256k1fx.OutputOwners{},
		),
		cmpopts.EquateEmpty(),
	})
}

func TestParse(t *testing.T) {
	for _, tx := range allGolden {
		t.Run(tx.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := parseOldTx(tx.bytes)
				require.NoError(t, err, "parseOldTx()")
				if diff := cmp.Diff(tx.old, got, oldCmpOpt()); diff != "" {
					t.Errorf("%T.Unmarshal(, %T) diff (-want +got):\n%s", atomic.Codec, got, diff)
				}
			})
			t.Run("new", func(t *testing.T) {
				got, err := Parse(tx.bytes)
				require.NoError(t, err, "Parse()")
				if diff := cmp.Diff(tx.new, got, txtest.CmpOpt()); diff != "" {
					t.Errorf("Parse() diff (-want +got):\n%s", diff)
				}
			})
		})
	}
}

// fuzz seeds f with [newTxs], specifies simple alphabets used to bias the
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
	for _, tx := range allGolden {
		fuzzer.Add(tx.new)
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

func FuzzParseCompatibility(f *testing.F) {
	for _, tx := range allGolden {
		f.Add(tx.bytes)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, oldErr := parseOldTx(data)
		oldOk := oldErr == nil

		_, newErr := Parse(data)
		newOk := newErr == nil

		assert.Equal(t, oldOk, newOk, "Parse(b) == parseOldTx(b)")
	})
}

func TestMarshalSlice(t *testing.T) {
	oldTxs := make([]*atomic.Tx, len(allGolden))
	newTxs := make([]*Tx, len(allGolden))
	for i, tx := range allGolden {
		oldTxs[i] = tx.old
		newTxs[i] = tx.new
	}

	want, err := atomic.Codec.Marshal(atomic.CodecVersion, oldTxs)
	require.NoErrorf(t, err, "%T.Marshal(, %T)", atomic.Codec, oldTxs)

	tests := []struct {
		name string
		txs  []*Tx
		want []byte
	}{
		{
			name: "mainnet",
			txs:  newTxs,
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
	oldTxs := make([]*atomic.Tx, len(allGolden))
	newTxs := make([]*Tx, len(allGolden))
	for i, tx := range allGolden {
		oldTxs[i] = tx.old
		newTxs[i] = tx.new
	}

	bytes, err := atomic.Codec.Marshal(atomic.CodecVersion, oldTxs)
	require.NoErrorf(t, err, "%T.Marshal(, %T)", atomic.Codec, oldTxs)

	tests := []struct {
		name    string
		bytes   []byte
		want    []*Tx
		wantErr error
	}{
		{
			name:  "mainnet",
			bytes: bytes,
			want:  newTxs,
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

func FuzzParseSliceRoundTrip(f *testing.F) {
	{
		newTxs := make([]*Tx, len(allGolden))
		for i, tx := range allGolden {
			newTxs[i] = tx.new
		}
		b, err := MarshalSlice(newTxs)
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

// parseOldTxs parses a slice of transactions using coreth's old parsing logic
// but enforces additional restrictions. Coreth's parsing logic is overly
// permissive and depends on later verification in [vm.VerifierBackend].
func parseOldTxs(b []byte) ([]*atomic.Tx, error) {
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

func FuzzParseSliceCompatibility(f *testing.F) {
	{
		newTxs := make([]*Tx, len(allGolden))
		for i, tx := range allGolden {
			newTxs[i] = tx.new
		}
		b, err := MarshalSlice(newTxs)
		require.NoError(f, err, "MarshalSlice()")
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_, oldErr := parseOldTxs(data)
		oldOk := oldErr == nil

		_, newErr := ParseSlice(data)
		newOk := newErr == nil

		assert.Equal(t, oldOk, newOk, "ParseSlice(b) == parseOldTxs(b)")
	})
}

func TestJSONMarshal(t *testing.T) {
	tests := []struct {
		tx   golden
		json string
	}{
		{
			tx: importGolden,
			json: `{
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
			tx: exportGolden,
			json: `{
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
			tx: importMultiInputGolden,
			json: `{
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
			tx: exportSameAddressMultiAssetGolden,
			json: `{
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
			tx: exportMultiAddressMultiAssetGolden,
			json: `{
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
			tx: importNonAVAXGolden,
			json: `{
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
		t.Run(test.tx.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				tx := test.tx.old
				got, err := json.Marshal(tx)
				require.NoErrorf(t, err, "json.Marshal(%T)", tx)
				assert.JSONEqf(t, test.json, string(got), "json.Marshal(%T)", tx)
			})
			t.Run("new", func(t *testing.T) {
				tx := test.tx.new
				got, err := json.Marshal(tx)
				require.NoErrorf(t, err, "json.Marshal(%T)", tx)
				assert.JSONEqf(t, test.json, string(got), "json.Marshal(%T)", tx)
			})
		})
	}
}

// toOldTx converts a transaction from the new format into coreth's old format.
func toOldTx(tb testing.TB, newTx *Tx) *atomic.Tx {
	tb.Helper()

	bytes, err := newTx.Bytes()
	require.NoErrorf(tb, err, "%T.Bytes()", newTx)

	oldTx, err := parseOldTx(bytes)
	require.NoError(tb, err, "parseOldTx()")
	return oldTx
}

func FuzzJSONCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		oldTx := toOldTx(t, newTx)
		want, err := json.Marshal(oldTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", oldTx)

		got, err := json.Marshal(newTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", newTx)
		assert.JSONEq(t, string(want), string(got))
	})
}
