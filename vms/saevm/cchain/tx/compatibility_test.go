// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txtest"

	. "github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// newFuzzF wraps f and seeds it with [NewTxs].
func newFuzzF(f *testing.F) *txtest.F {
	fuzzF := &txtest.F{
		F: f,
	}
	for _, tx := range NewTxs {
		fuzzF.Add(tx)
	}
	return fuzzF
}

func FuzzJSONCompatibility(f *testing.F) {
	newFuzzF(f).Fuzz(func(t *testing.T, newTx *Tx) {
		bytes, err := newTx.Bytes()
		require.NoErrorf(t, err, "%T.Bytes()", newTx)

		oldTx, err := ParseOldTx(bytes)
		require.NoError(t, err, "ParseOldTx()")

		oldJSON, err := json.Marshal(oldTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", oldTx)

		newJSON, err := json.Marshal(newTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", newTx)
		assert.JSONEq(t, string(oldJSON), string(newJSON))
	})
}
