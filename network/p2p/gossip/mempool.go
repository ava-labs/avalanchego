// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"

	xmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var ErrDuplicateTx = xmempool.ErrDuplicateTx

type Mempool[T Gossipable] interface {
	Add(tx T) error
	Remove(txs ...T)

	// Len returns the number of txs in the mempool.
	Len() int

	// Has returns true if the gossipable is in the set.
	Has(gossipID ids.ID) bool
}

type mempool[T Gossipable] struct {
	Txs     map[ids.ID]bool
	metrics *Metrics
}

func NewMempool[T Gossipable](
	metrics *Metrics,
) (*mempool[T], error) {
	return &mempool[T]{
		Txs:     make(map[ids.ID]bool),
		metrics: metrics,
	}, nil
}

func (m *mempool[T]) Add(tx T) error {
	txID := tx.GossipID()
	if m.Has(txID) {
		m.metrics.ObserveIncomingGossipable(txID, DroppedDuplicate)
		return fmt.Errorf("%w: %s", ErrDuplicateTx, txID)
	}

	m.Txs[txID] = true
	return nil
}

func (m *mempool[T]) Remove(txs ...T) {
	for _, tx := range txs {
		delete(m.Txs, tx.GossipID())
	}
}

func (m *mempool[T]) Len() int {
	return len(m.Txs)
}

// Has returns true if the gossipable is in the set.
func (m *mempool[T]) Has(gossipID ids.ID) bool {
	return m.Txs[gossipID]
}
