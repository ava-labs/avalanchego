package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

type SamplingFilter func(ctx context.Context, nodeID ids.NodeID) bool

// NewPeerValidatorFilter returns a filter that only samples other peer
// validators
func NewPeerValidatorFilter(self ids.NodeID, validators ValidatorSet) SamplingFilter {
	return func(ctx context.Context, nodeID ids.NodeID) bool {
		return nodeID != self && validators.Has(ctx, nodeID)
	}
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
		for _, filter := range p.filters {
			if !filter(ctx, nodeID) {
				continue
			}
		}

		sampled = append(sampled, nodeID)
	}

	return sampled
}
