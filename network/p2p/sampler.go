// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

var _ Sampler = (*UniformSampler)(nil)

// Sampler samples connected peers
type Sampler interface {
	// Sample returns a nodeID from nodeIDs
	// invariant: nodeIDs must not be modified
	Sample(ctx context.Context, nodeIDs []ids.NodeID) (ids.NodeID, bool)
}

// NewUniformSampler returns an instance of UniformSampler
func NewUniformSampler(filters ...SamplingFilter) *UniformSampler {
	return &UniformSampler{
		filters: filters,
	}
}

// UniformSampler samples peers uniformly
type UniformSampler struct {
	filters []SamplingFilter
}

// Sample returns a peer based on a pseudo-random uniform sample
func (u *UniformSampler) Sample(
	ctx context.Context,
	nodeIDs []ids.NodeID,
) (ids.NodeID, bool) {
	uniform := sampler.NewUniform()
	uniform.Initialize(uint64(len(nodeIDs)))

	for {
		i, err := uniform.Next()
		if err != nil {
			break
		}

		nodeID := nodeIDs[i]
		if !canSample(ctx, nodeID, u.filters...) {
			continue
		}

		return nodeID, true
	}

	return ids.EmptyNodeID, false
}

// canSample returns if nodeID matches all filters
func canSample(
	ctx context.Context,
	nodeID ids.NodeID,
	filters ...SamplingFilter,
) bool {
	for _, filter := range filters {
		if !filter.Filter(ctx, nodeID) {
			return false
		}
	}

	return true
}
