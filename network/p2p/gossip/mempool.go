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
	Gossipables map[ids.ID]struct{}
	metrics     *Metrics
}

func NewMempool[T Gossipable](
	metrics *Metrics,
) (*Mempool[T], error) {
	return &Mempool[T]{
		Gossipables: make(map[ids.ID]struct{}),
		metrics:     metrics,
	}, nil
}

func (m *Mempool[T]) Add(gossipable T) error {
	gossipableID := gossipable.GossipID()
	if m.Has(gossipableID) {
		m.metrics.AddDropMetric(gossipableID, DroppedDuplicate)
		return fmt.Errorf("%w: %s", ErrDuplicateTx, gossipableID)
	}

	m.Gossipables[gossipableID] = struct{}{}
	return nil
}

// Remove removes the given gossipableID from the Gossipables map.
func (m *Mempool[T]) Remove(gossipableID ids.ID) {
	delete(m.Gossipables, gossipableID)
}

// Has returns true if the gossipable is in the set.
func (m *Mempool[T]) Has(gossipID ids.ID) bool {
	_, ok := m.Gossipables[gossipID]
	return ok
}
