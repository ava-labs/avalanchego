// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"

	. "github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// fuzz seeds f with [NewTxs] and fuzzes the test.
func fuzz(f *testing.F, ff func(t *testing.T, tx *Tx)) {
	fuzzer := &txtest.F{
		F: f,
	}
	for _, tx := range NewTxs {
		fuzzer.Add(tx)
	}
	fuzzer.Fuzz(ff)
}

func FuzzParseRoundTrip(f *testing.F) {
	fuzz(f, func(t *testing.T, want *Tx) {
		bytes, err := want.Bytes()
		require.NoErrorf(t, err, "%T.Bytes()", want)

		got, err := Parse(bytes)
		require.NoError(t, err, "Parse()")
		if diff := cmp.Diff(want, got, CmpOpt()); diff != "" {
			t.Errorf("Parse() diff (-want +got):\n%s", diff)
		}
	})
}

func FuzzJSONCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		oldTx := ToOldTx(t, newTx)
		want, err := json.Marshal(oldTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", oldTx)

		got, err := json.Marshal(newTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", newTx)
		assert.JSONEq(t, string(want), string(got))
	})
}
