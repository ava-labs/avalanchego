// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ snowstorm.Tx = (*transactionVertex)(nil)

// newTransactionVertex returns a new transactionVertex initialized with a
// processing status.
func newTransactionVertex(vtx Vertex, nodes map[ids.ID]*transactionVertex) *transactionVertex {
	return &transactionVertex{
		vtx:    vtx,
		nodes:  nodes,
		status: choices.Processing,
	}
}

type transactionVertex struct {
	// vtx is the vertex that this transaction is attempting to confirm.
	vtx Vertex

	// nodes is used to look up other transaction vertices that are currently
	// processing. This is used to get parent vertices of this transaction.
	nodes map[ids.ID]*transactionVertex

	// status reports the status of this transaction vertex in snowstorm which
	// is then used by avalanche to determine the accaptability of the vertex.
	status choices.Status
}

func (*transactionVertex) Bytes() []byte {
	// Snowstorm uses the bytes of the transaction to broadcast through the
	// decision dispatcher. Because this is an internal transaction type, we
	// don't want to have this transaction broadcast. So, we return nil here.
	return nil
}

func (tv *transactionVertex) ID() ids.ID {
	return tv.vtx.ID()
}

func (tv *transactionVertex) Accept(context.Context) error {
	tv.status = choices.Accepted
	return nil
}

func (tv *transactionVertex) Reject(context.Context) error {
	tv.status = choices.Rejected
	return nil
}

func (tv *transactionVertex) Status() choices.Status {
	return tv.status
}

// Verify isn't called in the consensus code. So this implementation doesn't
// really matter. However it's used to implement the tx interface.
func (*transactionVertex) Verify(context.Context) error {
	return nil
}

// Dependencies returns the currently processing transaction vertices of this
// vertex's parents.
func (tv *transactionVertex) Dependencies() ([]snowstorm.Tx, error) {
	parents, err := tv.vtx.Parents()
	if err != nil {
		return nil, err
	}
	txs := make([]snowstorm.Tx, 0, len(parents))
	for _, parent := range parents {
		if parentTx, ok := tv.nodes[parent.ID()]; ok {
			txs = append(txs, parentTx)
		}
	}
	return txs, nil
}

// InputIDs must return a non-empty slice to avoid having the snowstorm engine
// vacuously accept it. A slice is returned containing just the vertexID in
// order to produce no conflicts based on the consumed input.
func (tv *transactionVertex) InputIDs() []ids.ID {
	return []ids.ID{tv.vtx.ID()}
}

func (tv *transactionVertex) HasWhitelist() bool {
	return tv.vtx.HasWhitelist()
}

func (tv *transactionVertex) Whitelist(ctx context.Context) (set.Set[ids.ID], error) {
	return tv.vtx.Whitelist(ctx)
}
