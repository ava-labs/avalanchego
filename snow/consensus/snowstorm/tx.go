// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// Whitelister defines the interface for specifying whitelisted operations.
type Whitelister interface {
	// Whitelist returns the set of transaction IDs that are explicitly
	// whitelisted. Transactions that are not explicitly whitelisted are
	// considered conflicting.
	//
	// Returns [false] if the underlying instance does not implement whitelisted
	// conflicts.
	Whitelist() (ids.Set, bool, error)
}

// Tx consumes state.
type Tx interface {
	choices.Decidable
	Whitelister

	// Dependencies is a list of transactions upon which this transaction
	// depends. Each element of Dependencies must be verified before Verify is
	// called on this transaction.
	//
	// Similarly, each element of Dependencies must be accepted before this
	// transaction is accepted.
	Dependencies() ([]Tx, error)

	// InputIDs is a set where each element is the ID of a piece of state that
	// will be consumed if this transaction is accepted.
	//
	// In the context of a UTXO-based payments system, for example, this would
	// be the IDs of the UTXOs consumed by this transaction
	InputIDs() []ids.ID

	// Verify that the state transition this transaction would make if it were
	// accepted is valid. If the state transition is invalid, a non-nil error
	// should be returned.
	//
	// It is guaranteed that when Verify is called, all the dependencies of
	// this transaction have already been successfully verified.
	Verify() error

	// Bytes returns the binary representation of this transaction.
	//
	// This is used for sending transactions to peers. Another node should be
	// able to parse these bytes to the same transaction.
	Bytes() []byte
}
