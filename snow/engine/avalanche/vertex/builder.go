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
