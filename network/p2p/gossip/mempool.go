// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var ErrDuplicateTx = errors.New("duplicate tx")

type Mempool[T Gossipable] struct {
	Gossipables set.Set[ids.ID]
	metrics     *Metrics
}

func NewMempool[T Gossipable](
	metrics *Metrics,
) (*Mempool[T], error) {
	return &Mempool[T]{
		Gossipables: set.NewSet[ids.ID](0),
		metrics:     metrics,
	}, nil
}

func (m *Mempool[T]) Add(gossipable T) error {
	gossipableID := gossipable.GossipID()
	if m.Has(gossipableID) {
		m.metrics.AddDropMetric(gossipableID, DroppedDuplicate)
		return fmt.Errorf("%w: %s", ErrDuplicateTx, gossipableID)
	}

	m.Gossipables.Add(gossipableID)
	return nil
}

// Remove removes the given gossipableID from the Gossipables set.
func (m *Mempool[T]) Remove(gossipableID ids.ID) {
	m.Gossipables.Remove(gossipableID)
}

// Has returns true if the gossipable is in the set.
func (m *Mempool[T]) Has(gossipID ids.ID) bool {
	return m.Gossipables.Contains(gossipID)
}
