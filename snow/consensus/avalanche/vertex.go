// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
)

// Vertex is a collection of multiple transactions tied to other vertices
type Vertex interface {
	choices.Decidable
	snowstorm.Whitelister

	// Vertex verification should be performed before issuance.
	Verify(context.Context) error

	// Returns the vertices this vertex depends on
	Parents() ([]Vertex, error)

	// Returns the height of this vertex. A vertex's height is defined by one
	// greater than the maximum height of the parents.
	Height() (uint64, error)

	// Returns a series of state transitions to be performed on acceptance
	Txs(context.Context) ([]snowstorm.Tx, error)

	// Returns the binary representation of this vertex
	Bytes() []byte
}
