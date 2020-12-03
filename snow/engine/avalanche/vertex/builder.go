// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
)

// Builder builds a vertex given a set of parentIDs and transactions.
type Builder interface {
	// Build a new vertex from the contents of a vertex
	Build(parentIDs []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error)
}

// Build a new vertex from the contents of a vertex
func Build(
	chainID ids.ID,
	height uint64,
	epoch uint32,
	parentIDs []ids.ID,
	txs [][]byte,
) (StatelessVertex, error) {
	if numParents := len(parentIDs); numParents > maxNumParents {
		return nil, fmt.Errorf("number of parents (%d) exceeds max (%d)", numParents, maxNumParents)
	} else if numParents > maxNumParents {
		return nil, fmt.Errorf("number of parents (%d) exceeds max (%d)", numParents, maxNumParents)
	} else if numTxs := len(txs); numTxs == 0 {
		return nil, errNoTxs
	} else if numTxs > maxTxsPerVtx {
		return nil, fmt.Errorf("number of txs (%d) exceeds max (%d)", l, maxTxsPerVtx)
	}

	ids.SortIDs(parentIDs)
	SortHashOf(txs)

	height := uint64(0)
	for _, parentID := range parentIDs {
		parent, err := s.getVertex(parentID)
		if err != nil {
			return nil, err
		}
		height = math.Max64(height, parent.v.vtx.height)
	}

	vtx := &innerVertex{
		chainID:   s.ctx.ChainID,
		height:    height + 1,
		parentIDs: parentIDs,
		txs:       txs,
	}

	bytes, err := vtx.Marshal()
	if err != nil {
		return nil, err
	}
	vtx.bytes = bytes
	vtx.id = hashing.ComputeHash256Array(vtx.bytes)

	uVtx := &uniqueVertex{
		serializer: s,
		vtxID:      vtx.ID(),
	}
	// setVertex handles the case where this vertex already exists even
	// though we just made it
	return uVtx, uVtx.setVertex(vtx)
}
