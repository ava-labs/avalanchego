// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Gossipable = (*dummyGossipable)(nil)

type dummyGossipable struct {
	id ids.ID
}

func (g *dummyGossipable) GossipID() ids.ID {
	return g.id
}

func newMempool[T Gossipable](t *testing.T) *Mempool[T] {
	require := require.New(t)
	metrics, err := NewMetrics(prometheus.NewRegistry(), "gossip_metrics")
	require.NoError(err)
	mp, err := NewMempool[T](metrics)
	require.NoError(err)
	return mp
}

func TestAdd(t *testing.T) {
	gossipable0 := &dummyGossipable{
		id: ids.GenerateTestID(),
	}
	gossipable1 := &dummyGossipable{
		id: ids.GenerateTestID(),
	}
	tests := []struct {
		name               string
		initialGossipables []*dummyGossipable
		gossipable         *dummyGossipable
		err                error
		dropReason         error
	}{
		{
			name:               "successfully add tx",
			initialGossipables: nil,
			gossipable:         gossipable0,
			err:                nil,
			dropReason:         nil,
		},
		{
			name:               "successfully add tx second time",
			initialGossipables: []*dummyGossipable{gossipable0},
			gossipable:         gossipable1,
			err:                nil,
			dropReason:         nil,
		},
		{
			name:               "attempt adding duplicate tx",
			initialGossipables: []*dummyGossipable{gossipable0},
			gossipable:         gossipable0,
			err:                ErrDuplicateTx,
			dropReason:         nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			mempool := newMempool[*dummyGossipable](t)

			for _, gossipable := range test.initialGossipables {
				require.NoError(mempool.Add(gossipable))
			}

			err := mempool.Add(test.gossipable)
			require.ErrorIs(err, test.err)
		})
	}
}
