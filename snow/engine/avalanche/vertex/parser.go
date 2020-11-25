// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

// Parser parses bytes into a vertex.
type Parser interface {
	// Attempt to convert a stream of bytes into a vertex
	ParseVertex(vertex []byte) (avalanche.Vertex, error)
}
