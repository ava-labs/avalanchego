// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/snow/choices"
)

// Tx consumes state.
type Tx interface {
	choices.Decidable

	// Dependencies is a list of transactions upon which this transaction
	// depends. Each element of Dependencies must be verified before Verify is
	// called on this transaction.
	//
	// Similarly, each element of Dependencies must be accepted before this
	// transaction is accepted.
	Dependencies() ([]Tx, error)

	// Bytes returns the binary representation of this transaction.
	//
	// This is used for sending transactions to peers. Another node should be
	// able to parse these bytes to the same transaction.
	Bytes() []byte
}
