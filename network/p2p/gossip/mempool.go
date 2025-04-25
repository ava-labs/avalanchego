// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var ErrDuplicateTx = errors.New("duplicate tx")

type Mempool[T Gossipable] struct {
	Txs     map[ids.ID]bool
	metrics *Metrics
}

func NewMempool[T Gossipable](
	metrics *Metrics,
) (*Mempool[T], error) {
	return &Mempool[T]{
		Txs:     make(map[ids.ID]bool),
		metrics: metrics,
	}, nil
}

func (m *Mempool[T]) Add(gossipable T) error {
	txID := gossipable.GossipID()
	if m.Has(txID) {
		m.metrics.ObserveIncomingGossipable(txID, DroppedDuplicate)
		return fmt.Errorf("%w: %s", ErrDuplicateTx, txID)
	}

	m.Txs[txID] = true
	return nil
}

func (m *Mempool[T]) Remove(gossipables ...T) {
	for _, tx := range gossipables {
		delete(m.Txs, tx.GossipID())
	}
}

// Has returns true if the gossipable is in the set.
func (m *Mempool[T]) Has(gossipID ids.ID) bool {
	return m.Txs[gossipID]
}
