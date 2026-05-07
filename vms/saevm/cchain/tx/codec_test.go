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

func TestID(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				// We must parse the old tx to properly initialize the ID.
				old, err := parseOldTx(test.bytes)
				require.NoError(t, err, "parseOldTx()")
				assert.Equalf(t, test.op.ID, old.ID(), "%T.ID()", old)
			})
			t.Run("new", func(t *testing.T) {
				assert.Equalf(t, test.op.ID, test.new.ID(), "%T.ID()", test.new)
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
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				got, err := parseOldTx(test.bytes)
				require.NoError(t, err, "parseOldTx()")
				if diff := cmp.Diff(test.old, got, oldCmpOpt()); diff != "" {
					t.Errorf("%T.Unmarshal(, %T) diff (-want +got):\n%s", atomic.Codec, got, diff)
				}
			})
			t.Run("new", func(t *testing.T) {
				got, err := Parse(test.bytes)
				require.NoError(t, err, "Parse()")
				if diff := cmp.Diff(test.new, got, txtest.CmpOpt()); diff != "" {
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
	for _, tx := range tests {
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

func TestMarshalSlice(t *testing.T) {
	oldTxs := make([]*atomic.Tx, len(tests))
	newTxs := make([]*Tx, len(tests))
	for i, test := range tests {
		oldTxs[i] = test.old
		newTxs[i] = test.new
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
	oldTxs := make([]*atomic.Tx, len(tests))
	newTxs := make([]*Tx, len(tests))
	for i, test := range tests {
		oldTxs[i] = test.old
		newTxs[i] = test.new
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
		newTxs := make([]*Tx, len(tests))
		for i, test := range tests {
			newTxs[i] = test.new
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
		newTxs := make([]*Tx, len(tests))
		for i, test := range tests {
			newTxs[i] = test.new
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
