// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txtest

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	// Imported for [vm.VerifierBackend] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errUnexpectedCredentialType = errors.New("unexpected credential type")

// ParseOld parses a transaction using coreth's old parsing logic while
// enforcing restrictions imposed by the new parsing logic.
//
// Coreth's parsing logic is overly permissive and depends on later verification
// in [vm.VerifierBackend].
func ParseOld(b []byte) (*atomic.Tx, error) {
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

// ParseOlds parses a slice of transaction using coreth's old parsing logic
// while enforcing restrictions imposed by the new parsing logic.
//
// Coreth's parsing logic is overly permissive and depends on later verification
// in [vm.VerifierBackend].
func ParseOlds(b []byte) ([]*atomic.Tx, error) {
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

// ToOld converts a transaction from the new format into coreth's old format.
func ToOld(tb testing.TB, newTx *tx.Tx) *atomic.Tx {
	tb.Helper()

	bytes, err := newTx.Bytes()
	require.NoErrorf(tb, err, "%T.Bytes()", newTx)

	oldTx, err := ParseOld(bytes)
	require.NoError(tb, err, "ParseOld()")
	return oldTx
}

// ToOlds converts a slice of transactions from the new format into coreth's old
// format.
func ToOlds(tb testing.TB, newTxs []*tx.Tx) []*atomic.Tx {
	tb.Helper()

	oldTxs := make([]*atomic.Tx, len(newTxs))
	for i, newTx := range newTxs {
		oldTxs[i] = ToOld(tb, newTx)
	}
	return oldTxs
}

// ToNew converts a transaction from coreth's old format into the new format.
func ToNew(tb testing.TB, oldTx *atomic.Tx) *tx.Tx {
	tb.Helper()

	// Don't use [atomic.Tx.SignedBytes] in case it wasn't properly initialized.
	bytes, err := atomic.Codec.Marshal(atomic.CodecVersion, oldTx)
	require.NoErrorf(tb, err, "%T.Marshal(, %T)", atomic.Codec, oldTx)
	newTx, err := tx.Parse(bytes)
	require.NoError(tb, err, "tx.Parse()")
	return newTx
}

// ToNews converts a slice of transactions from coreth's old format into the new
// format.
func ToNews(tb testing.TB, oldTxs []*atomic.Tx) []*tx.Tx {
	tb.Helper()

	newTxs := make([]*tx.Tx, len(oldTxs))
	for i, oldTx := range oldTxs {
		newTxs[i] = ToNew(tb, oldTx)
	}
	return newTxs
}
