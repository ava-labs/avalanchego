// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/builder"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

const (
	bloomChurnMultiplier           = 3
	minTargetElements              = 1000
	targetFalsePositiveProbability = 0.01
	resetFalsePositiveProbability  = 0.1
)

func NewMempool(
	namespace string,
	registerer prometheus.Registerer,
) (mempool.Mempool[*tx.Tx], error) {
	metrics, err := mempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	return mempool.New[*tx.Tx](metrics), nil
}

type GossipMempool struct {
	builder.Builder
	lock sync.RWMutex

	mempool mempool.Mempool[*tx.Tx]
	bloom   *gossip.BloomFilter
}

var _ gossip.Set[*tx.Tx] = (*GossipMempool)(nil)

func NewGossipMempool(
	mempool mempool.Mempool[*tx.Tx],
	registerer prometheus.Registerer,
	builder builder.Builder,
) (*GossipMempool, error) {
	bloom, err := gossip.NewBloomFilter(registerer, "mempool_bloom_filter", minTargetElements, targetFalsePositiveProbability, resetFalsePositiveProbability)

	return &GossipMempool{
		mempool: mempool,
		bloom:   bloom,
		Builder: builder,
	}, err
}

func (g *GossipMempool) Has(id ids.ID) bool {
	_, exists := g.mempool.Get(id)
	return exists
}

func (g *GossipMempool) Add(t *tx.Tx) error {
	return g.AddWithContext(context.Background(), t)
}

func (g *GossipMempool) AddWithContext(ctx context.Context, t *tx.Tx) error {
	// no need to add to g.mempool, since the builder adds to g.mempool via its reference
	err := g.Builder.AddTx(ctx, t)
	if err != nil {
		return fmt.Errorf("failed to add tx to builder: %w", err)
	}

	// note: we do not verify transactions that are gossiped to us.
	g.lock.Lock()
	defer g.lock.Unlock()

	g.bloom.Add(t)
	reset, err := gossip.ResetBloomFilterIfNeeded(g.bloom, g.mempool.Len()*bloomChurnMultiplier)
	if err != nil {
		return err
	}

	if reset {
		g.mempool.Iterate(func(tx *tx.Tx) bool {
			g.bloom.Add(tx)
			return true
		})
	}

	return nil
}

func (g *GossipMempool) GetFilter() ([]byte, []byte) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.bloom.Marshal()
}

func (g *GossipMempool) Iterate(f func(*tx.Tx) bool) {
	g.mempool.Iterate(f)
}
