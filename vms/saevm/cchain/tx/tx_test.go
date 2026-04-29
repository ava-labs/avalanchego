// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Imported for [parseOldTx] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// tests is defined at the package level to allow sharing between fuzz tests and
// unit tests.
var (
	tests = [...]struct {
		name  string
		old   *atomic.Tx
		new   *Tx
		json  string
		id    ids.ID
		bytes []byte
	}{
		{
			name: "import", // Included in https://subnets.avax.network/c-chain/block/4
			old: &atomic.Tx{
				UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    1,
					BlockchainID: ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"),
					SourceChain:  ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"),
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{
							TxID:        ids.FromStringOrPanic("2VqSFA5hxukiv1FSAB8ShjwHwmPev9ZS8VD9aUTCDRoff7T5Bi"),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
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
						AssetID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
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
			new: &Tx{
				Unsigned: &Import{
					NetworkID:    1,
					BlockchainID: ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"),
					SourceChain:  ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"),
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{
							TxID:        ids.FromStringOrPanic("2VqSFA5hxukiv1FSAB8ShjwHwmPev9ZS8VD9aUTCDRoff7T5Bi"),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
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
						AssetID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
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
			id:    ids.FromStringOrPanic("h34BPNmYApCbW8buVWAtzu1KtjTFmyMhiRQQnAqPqwCqQsB7f"),
			bytes: common.FromHex("0x000000000000000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b00000001c52b712aa7dce27a650bf509f799673e245edd4fa9e4e1700eb6105202fe579a0000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000002faf080000000010000000000000001b8b5a87d1c05676f1f966da49151fa54dbe68c330000000002faf08021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000100000009000000013e6614876ee01d3b8b27480c00bdcb0ae84ee3e8346d2d5f08320f7dd3e76c4540be021fe85e91817654c9310b54e8f2e88d81db52b8693842b90f3dbd23bd5c01"),
		},
		{
			name: "export", // Included in https://subnets.avax.network/c-chain/block/48
			old: &atomic.Tx{
				UnsignedAtomicTx: &atomic.UnsignedExportTx{
					NetworkID:        1,
					BlockchainID:     ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"),
					DestinationChain: ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"),
					Ins: []atomic.EVMInput{{
						Address: common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"),
						Amount:  1000001,
						AssetID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
					}},
					ExportedOutputs: []*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
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
			new: &Tx{
				Unsigned: &Export{
					NetworkID:        1,
					BlockchainID:     ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"),
					DestinationChain: ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"),
					Ins: []Input{{
						Address: common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"),
						Amount:  1000001,
						AssetID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
					}},
					ExportedOutputs: []*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"),
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
			id:    ids.FromStringOrPanic("ng7Dox1r8nctrF6zurhRPYWxkmE2juUhT7Qhpauyo8qSEu6jB"),
			bytes: common.FromHex("0x000000000001000000010427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd652ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b00000001eb019ccd325ad53543a7e7e3b04828bdecf3cff600000000000f424121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000000000000000000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000000000000000100000000000000000000000100000001d6ce17826dd7c12a7577af257e82d99143b72500000000010000000900000001254d11f1adbd5dfb556855d02ac236ea2dd45d1463459b73714f55ab8d34a4b74a1f18c2868b886e83a5463c422ea3ccc7e9783d5620b1f5695646b0cb1e4dfa01"),
		},
	}
	oldTxs []*atomic.Tx
	newTxs []*Tx
)

func init() {
	oldTxs = make([]*atomic.Tx, len(tests))
	newTxs = make([]*Tx, len(tests))
	for i, test := range tests {
		oldTxs[i] = test.old
		newTxs[i] = test.new
	}
}

func TestID(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				// We must parse the old tx to properly initialize the ID.
				old, err := parseOldTx(test.bytes)
				require.NoError(t, err, "parseOldTx()")
				assert.Equalf(t, test.id, old.ID(), "%T.ID()", old)
			})
			t.Run("new", func(t *testing.T) {
				assert.Equalf(t, test.id, test.new.ID(), "%T.ID()", test.new)
			})
		})
	}
}

func TestBytes(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := atomic.Codec.Marshal(atomic.CodecVersion, test.old)
				require.NoErrorf(t, err, "%T.Marshal(, %T)", atomic.Codec, test.old)
				assert.Equalf(t, test.bytes, got, "%T.Marshal(, %T)", atomic.Codec, test.old)
			})
			t.Run("new", func(t *testing.T) {
				got, err := test.new.Bytes()
				require.NoErrorf(t, err, "%T.Bytes()", test.new)
				assert.Equalf(t, test.bytes, got, "%T.Bytes()", test.new)
			})
		})
	}
}

func TestMarshalSlice(t *testing.T) {
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

func TestParse(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got := new(atomic.Tx)
				_, err := atomic.Codec.Unmarshal(test.bytes, got)
				require.NoErrorf(t, err, "%T.Unmarshal(, %T)", atomic.Codec, got)
				assert.Equalf(t, test.old, got, "%T.Unmarshal(, %T)", atomic.Codec, got)
			})
			t.Run("new", func(t *testing.T) {
				got, err := Parse(test.bytes)
				require.NoError(t, err, "Parse()")
				assert.Equal(t, test.new, got, "Parse()")
			})
		})
	}
}

func TestParseSlice(t *testing.T) {
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
			wantErr: errInefficientSlicePacking,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseSlice(test.bytes)
			require.ErrorIs(t, err, test.wantErr, "ParseSlice()")
			assert.Equal(t, test.want, got, "ParseSlice()")
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

func FuzzParseCompatibility(f *testing.F) {
	for _, test := range tests {
		f.Add(test.bytes)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, oldErr := parseOldTx(data)
		oldOk := oldErr == nil

		_, newErr := Parse(data)
		newOk := newErr == nil

		assert.Equal(t, oldOk, newOk, "Parse(b) == parseOldTx(b)")
	})
}

func FuzzParseSliceCompatibility(f *testing.F) {
	{
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

func FuzzParseRoundTrip(f *testing.F) {
	for _, test := range tests {
		f.Add(test.bytes)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		tx, err := Parse(data)
		if err != nil {
			return
		}

		got, err := tx.Bytes()
		require.NoErrorf(t, err, "%T.Bytes()", tx)
		assert.Equal(t, data, got, "Parse(b).Bytes() == b")
	})
}

func FuzzParseSliceRoundTrip(f *testing.F) {
	{
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

func TestJSONMarshal(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := json.Marshal(test.old)
				require.NoErrorf(t, err, "json.Marshal(%T)", test.old)
				assert.JSONEqf(t, test.json, string(got), "json.Marshal(%T)", test.old)
			})
			t.Run("new", func(t *testing.T) {
				got, err := json.Marshal(test.new)
				require.NoErrorf(t, err, "json.Marshal(%T)", test.new)
				assert.JSONEqf(t, test.json, string(got), "json.Marshal(%T)", test.new)
			})
		})
	}
}

func FuzzJSONCompatibility(f *testing.F) {
	for _, test := range tests {
		f.Add(test.bytes)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		oldTx, err := parseOldTx(data)
		if err != nil {
			t.Skip("invalid tx")
		}

		newTx, err := Parse(data)
		require.NoError(t, err, "Parse()")

		want, err := json.Marshal(oldTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", oldTx)

		got, err := json.Marshal(newTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", newTx)
		assert.JSONEq(t, string(want), string(got))
	})
}
