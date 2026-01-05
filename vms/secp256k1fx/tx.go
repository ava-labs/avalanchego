// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

// UnsignedTx that this Fx is supporting
type UnsignedTx interface {
	Bytes() []byte
}

var _ UnsignedTx = (*TestTx)(nil)

// TestTx is a minimal implementation of a Tx
type TestTx struct{ UnsignedBytes []byte }

// Bytes returns UnsignedBytes
func (tx *TestTx) Bytes() []byte {
	return tx.UnsignedBytes
}
