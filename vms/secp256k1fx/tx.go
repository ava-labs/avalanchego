// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

// Tx that this Fx is supporting
type Tx interface {
	UnsignedBytes() []byte
}

var _ Tx = &TestTx{}

// TestTx is a minimal implementation of a Tx
type TestTx struct{ Bytes []byte }

// UnsignedBytes returns Bytes
func (tx *TestTx) UnsignedBytes() []byte { return tx.Bytes }
