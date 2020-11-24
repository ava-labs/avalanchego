// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
)

// Builder builds a vertex given a set of parentIDs and transactions.
type Builder interface {
	// Create a new vertex from the contents of a vertex
	BuildVertex(parentIDs []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error)
}

// Parser parses bytes into a vertex.
type Parser interface {
	// Attempt to convert a stream of bytes into a vertex
	ParseVertex(vertex []byte) (avalanche.Vertex, error)
}

// Storage defines the persistent storage that is required by the consensus
// engine.
type Storage interface {
	// GetVertex attempts to load a vertex by hash from storage.
	GetVertex(vtxID ids.ID) (avalanche.Vertex, error)

	// Edge returns a list of accepted vertex IDs with no accepted children.
	Edge() (vtxIDs []ids.ID)
}

// Manager defines all the vertex related functionality that is required by the
// consensus engine.
type Manager interface {
	Builder
	Parser
	Storage
}
