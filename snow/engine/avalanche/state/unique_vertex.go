// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// uniqueVertex acts as a cache for vertices in the database.
//
// If a vertex is loaded, it will have one canonical uniqueVertex. The vertex
// will eventually be evicted from memory, when the uniqueVertex is evicted from
// the cache. If the uniqueVertex has a function called again afther this
// eviction, the vertex will be re-loaded from the database.
type uniqueVertex struct {
	serializer *Serializer

	vtxID ids.ID
	v     *vertexState
}

func (vtx *uniqueVertex) refresh() {
	if vtx.v == nil {
		vtx.v = &vertexState{}
	}
	if vtx.v.unique {
		return
	}

	unique := vtx.serializer.state.UniqueVertex(vtx)
	prevVtx := vtx.v.vtx
	if unique == vtx {
		vtx.v.status = vtx.serializer.state.Status(vtx.ID())
		vtx.v.unique = true
	} else {
		// If someone is in the cache, they must be up to date
		*vtx = *unique
	}

	switch {
	case vtx.v.vtx == nil && prevVtx == nil:
		vtx.v.vtx = vtx.serializer.state.Vertex(vtx.ID())
	case vtx.v.vtx == nil:
		vtx.v.vtx = prevVtx
	}
}

func (vtx *uniqueVertex) Evict() {
	if vtx.v != nil {
		vtx.v.unique = false
		// make sure the parents can be garbage collected
		vtx.v.parents = nil
	}
}

func (vtx *uniqueVertex) setVertex(innerVtx *innerVertex) error {
	vtx.refresh()
	if vtx.v.vtx != nil {
		return nil
	}
	vtx.v.vtx = innerVtx
	if err := vtx.serializer.state.SetVertex(innerVtx); err != nil {
		return err
	}
	return vtx.setStatus(choices.Processing)
}

func (vtx *uniqueVertex) setStatus(status choices.Status) error {
	vtx.refresh()
	if vtx.v.status == status {
		return nil
	}
	vtx.v.status = status
	return vtx.serializer.state.SetStatus(vtx.ID(), status)
}

func (vtx *uniqueVertex) ID() ids.ID { return vtx.vtxID }

func (vtx *uniqueVertex) Accept() error {
	if err := vtx.setStatus(choices.Accepted); err != nil {
		return err
	}

	vtx.serializer.edge.Add(vtx.vtxID)
	parents, err := vtx.Parents()
	if err != nil {
		return err
	}

	for _, parent := range parents {
		vtx.serializer.edge.Remove(parent.ID())
	}

	if err := vtx.serializer.state.SetEdge(vtx.serializer.edge.List()); err != nil {
		return fmt.Errorf("failed to set edge while accepting vertex %s due to %w", vtx.vtxID, err)
	}

	// Should never traverse into parents of a decided vertex. Allows for the
	// parents to be garbage collected
	vtx.v.parents = nil

	return vtx.serializer.db.Commit()
}

func (vtx *uniqueVertex) Reject() error {
	if err := vtx.setStatus(choices.Rejected); err != nil {
		return err
	}

	// Should never traverse into parents of a decided vertex. Allows for the
	// parents to be garbage collected
	vtx.v.parents = nil

	return vtx.serializer.db.Commit()
}

func (vtx *uniqueVertex) Status() choices.Status { vtx.refresh(); return vtx.v.status }

func (vtx *uniqueVertex) Parents() ([]avalanche.Vertex, error) {
	vtx.refresh()

	if vtx.v.vtx == nil {
		return nil, fmt.Errorf("failed to get parents for vertex with status: %s", vtx.v.status)
	}

	if len(vtx.v.parents) != len(vtx.v.vtx.parentIDs) {
		vtx.v.parents = make([]avalanche.Vertex, len(vtx.v.vtx.parentIDs))
		for i, parentID := range vtx.v.vtx.parentIDs {
			vtx.v.parents[i] = &uniqueVertex{
				serializer: vtx.serializer,
				vtxID:      parentID,
			}
		}
	}

	return vtx.v.parents, nil
}

func (vtx *uniqueVertex) Height() (uint64, error) {
	vtx.refresh()

	if vtx.v.vtx == nil {
		return 0, fmt.Errorf("failed to get height for vertex with status: %s", vtx.v.status)
	}

	return vtx.v.vtx.height, nil
}

func (vtx *uniqueVertex) Txs() ([]snowstorm.Tx, error) {
	vtx.refresh()

	if vtx.v.vtx == nil {
		return nil, fmt.Errorf("failed to get txs for vertex with status: %s", vtx.v.status)
	}

	if len(vtx.v.vtx.txs) != len(vtx.v.txs) {
		vtx.v.txs = make([]snowstorm.Tx, len(vtx.v.vtx.txs))
		for i, tx := range vtx.v.vtx.txs {
			vtx.v.txs[i] = tx
		}
	}

	return vtx.v.txs, nil
}

func (vtx *uniqueVertex) Bytes() []byte { return vtx.v.vtx.Bytes() }

func (vtx *uniqueVertex) Verify() error { return vtx.v.vtx.Verify() }

func (vtx *uniqueVertex) String() string {
	sb := strings.Builder{}

	parents, err := vtx.Parents()
	if err != nil {
		sb.WriteString(fmt.Sprintf("Vertex(ID = %s, Error=error while retrieving vertex parents: %s)", vtx.ID(), err))
		return sb.String()
	}
	txs, err := vtx.Txs()
	if err != nil {
		sb.WriteString(fmt.Sprintf("Vertex(ID = %s, Error=error while retrieving vertex txs: %s)", vtx.ID(), err))
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf(
		"Vertex(ID = %s, Status = %s, Number of Dependencies = %d, Number of Transactions = %d)",
		vtx.ID(),
		vtx.Status(),
		len(parents),
		len(txs),
	))

	parentFormat := fmt.Sprintf("\n    Parent[%s]: ID = %%s, Status = %%s",
		formatting.IntFormat(len(parents)-1))
	for i, parent := range parents {
		sb.WriteString(fmt.Sprintf(parentFormat, i, parent.ID(), parent.Status()))
	}

	txFormat := fmt.Sprintf("\n    Transaction[%s]: ID = %%s, Status = %%s",
		formatting.IntFormat(len(txs)-1))
	for i, tx := range txs {
		sb.WriteString(fmt.Sprintf(txFormat, i, tx.ID(), tx.Status()))
	}

	return sb.String()
}

type vertexState struct {
	unique bool

	vtx    *innerVertex
	status choices.Status

	parents []avalanche.Vertex
	txs     []snowstorm.Tx
}
