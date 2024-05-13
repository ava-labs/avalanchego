// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

type SamplingFilter interface {
	// Filter returns if nodeID can be sampled
	Filter(ctx context.Context, nodeID ids.NodeID) bool
}

func newPeerSampler(peers *Peers, filters ...SamplingFilter) peerSampler {
	return peerSampler{
		peers:   peers,
		filters: filters,
	}

}

type peerSampler struct {
	peers   *Peers
	filters []SamplingFilter
}

func (p peerSampler) Sample(ctx context.Context, limit int) []ids.NodeID {
	p.peers.lock.RLock()
	defer p.peers.lock.RUnlock()

	uniform := sampler.NewUniform()
	uniform.Initialize(uint64(len(p.peers.set.Elements)))

	sampled := make([]ids.NodeID, 0, limit)

	for len(sampled) < limit {
		i, err := uniform.Next()
		if err != nil {
			break
		}

		nodeID := p.peers.set.Elements[i]
		if !p.canSample(ctx, nodeID) {
			continue
		}

		sampled = append(sampled, nodeID)
	}

	return sampled
}

func (p peerSampler) canSample(ctx context.Context, nodeID ids.NodeID) bool {
	for _, filter := range p.filters {
		if !filter.Filter(ctx, nodeID) {
			return false
		}
	}

	return true
}
