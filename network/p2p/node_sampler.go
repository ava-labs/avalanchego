// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// NodeSampler samples nodes in network
type NodeSampler interface {
	// Sample returns at most [limit] nodes. This may return fewer nodes if
	// fewer than [limit] are available. The returned nodes must satisfy the
	// [canReturn] predicate.
	Sample(
		ctx context.Context,
		canReturn func(ids.NodeID) bool,
		limit int,
	) []ids.NodeID
}

// RestrictSampler returns a sampler that only returns nodes that satisfy
// [canReturn].
func RestrictSampler(s NodeSampler, canReturn func(ids.NodeID) bool) NodeSampler {
	return &restrictedSampler{
		s:         s,
		canReturn: canReturn,
	}
}

type restrictedSampler struct {
	s         NodeSampler
	canReturn func(ids.NodeID) bool
}

func (s *restrictedSampler) Sample(
	ctx context.Context,
	canReturn func(ids.NodeID) bool,
	limit int,
) []ids.NodeID {
	return s.s.Sample(
		ctx,
		func(nodeID ids.NodeID) bool {
			return s.canReturn(nodeID) && canReturn(nodeID)
		},
		limit,
	)
}
