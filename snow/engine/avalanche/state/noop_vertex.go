// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/utils/formatting"
)

// noopVertex acts as wrapper for stateless vertices with no txs
type noopVertex struct {
	serializer *Serializer

	vtxID ids.ID
	v     *noopState
}

func (vtx *noopVertex) setVertex(innerVtx *vertex) {
	if vtx.v.vtx == nil {
		vtx.v.vtx = innerVtx
                vtx.setStatus(choices.Processing)
	}
}

func (vtx *noopVertex) setStatus(status choices.Status) {
	if vtx.v.status != status {
		vtx.v.status = status
	}
}

// should this retern vtx.v.ID
func (vtx *noopVertex) ID() ids.ID { return vtx.vtxID }

func (vtx *noopVertex) Accept() {
	vtx.setStatus(choices.Accepted)

	vtx.serializer.edge.Add(vtx.vtxID)
	for _, parent := range vtx.Parents() {
		vtx.serializer.edge.Remove(parent.ID())
	}
	// Should never traverse into parents of a decided vertex. Allows for the
	// parents to be garbage collected
	vtx.v.parents = nil
}

func (vtx *noopVertex) Reject() {
	vtx.setStatus(choices.Rejected)

	// Should never traverse into parents of a decided vertex. Allows for the
	// parents to be garbage collected
	vtx.v.parents = nil
}

func (vtx *noopVertex) Status() choices.Status { return vtx.v.status }

func (vtx *noopVertex) Parents() []avalanche.Vertex {

	if len(vtx.v.parents) != len(vtx.v.vtx.parentIDs) {
		vtx.v.parents = make([]avalanche.Vertex, len(vtx.v.vtx.parentIDs))
		for i, parentID := range vtx.v.vtx.parentIDs {
			vtx.v.parents[i] = &noopVertex{
				serializer: vtx.serializer,
				vtxID:      parentID,
			}
		}
	}

	return vtx.v.parents
}

func (vtx *noopVertex) Txs() []snowstorm.Tx {

	if len(vtx.v.vtx.txs) != len(vtx.v.txs) {
		vtx.v.txs = make([]snowstorm.Tx, len(vtx.v.vtx.txs))
		for i, tx := range vtx.v.vtx.txs {
			vtx.v.txs[i] = tx
		}
	}

	return vtx.v.txs
}

func (vtx *noopVertex) Bytes() []byte { return vtx.v.vtx.Bytes() }

func (vtx *noopVertex) Verify() error { return vtx.v.vtx.Verify() }

func (vtx *noopVertex) String() string {
	sb := strings.Builder{}

	parents := vtx.Parents()
	txs := vtx.Txs()

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

type noopState struct {
	vtx    *vertex
	status choices.Status

	parents []avalanche.Vertex
	txs     []snowstorm.Tx
}
