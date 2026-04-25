// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package tx defines the Avalanche-specific transaction types used on the
// C-Chain to interact with the shared memory between the C-Chain and other
// chains on the Primary Network.
package tx

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Tx is a signed transaction that interacts with shared memory.
// The [Unsigned] body can be implemented by either [Export] or [Import].
// The [Credential] values are implemented by [secp256k1fx.Credential].
type Tx struct {
	Unsigned `serialize:"true" json:"unsignedTx"`
	Creds    []Credential `serialize:"true" json:"credentials"`
}

// Unsigned is a common interface implemented by [Import] and [Export].
//
// TODO(StephenButtolph): Expand this interface to include UTXO handling,
// verification, and state execution.
type Unsigned interface {
	// This function ensures that [Tx.Unsigned] can only be parsed as [Export]
	// or [Import].
	//
	// TODO(StephenButtolph): Once [Unsigned] includes other unexported
	// functions, remove this function.
	isUnsigned()
}

// Credential is used in [Tx] to authorize an input of a transaction.
//
// It is only implemented by [secp256k1fx.Credential]. An interface must be used
// to correctly produce the canonical binary format during serialization.
type Credential interface {
	Self() *secp256k1fx.Credential
}

// ID returns the unique hash of the transaction.
func (t *Tx) ID() ids.ID {
	// TODO(StephenButtolph): Optimize ID by caching previously calculated
	// values.
	bytes, err := t.Bytes()
	// This error can happen, but only with invalid transactions. To avoid
	// polluting the interface, we represent all invalid transactions with
	// the zero ID.
	if err != nil {
		return ids.ID{}
	}
	return hashing.ComputeHash256Array(bytes)
}

// Bytes returns the canonical binary format of the transaction.
func (t *Tx) Bytes() ([]byte, error) {
	// TODO(StephenButtolph): Optimize Bytes by caching previously calculated
	// values.
	return c.Marshal(codecVersion, t)
}

// Parse deserializes a [Tx] from its canonical binary format.
func Parse(b []byte) (*Tx, error) {
	var tx Tx
	if _, err := c.Unmarshal(b, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

// MarshalSlice returns the canonical binary format of a slice of transactions.
func MarshalSlice(txs []*Tx) ([]byte, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	return c.Marshal(codecVersion, txs)
}

var errInefficientSlicePacking = errors.New("inefficient slice packing: empty slices should be packed as nil")

// ParseSlice deserializes a slice of [Tx] from its canonical binary format.
func ParseSlice(b []byte) ([]*Tx, error) {
	if len(b) == 0 {
		return nil, nil
	}

	var txs []*Tx
	if _, err := c.Unmarshal(b, &txs); err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, errInefficientSlicePacking
	}
	return txs, nil
}
