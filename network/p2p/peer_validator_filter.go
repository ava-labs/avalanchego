// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ SamplingFilter = (*PeerSamplingFilter)(nil)
)

// NewPeerSamplingFilter returns an instance of PeerSamplingFilter
func NewPeerSamplingFilter(self ids.NodeID) *PeerSamplingFilter {
	return &PeerSamplingFilter{self: self}
}

// PeerSamplingFilter only allows sampling other peers
type PeerSamplingFilter struct {
	self ids.NodeID
}

func (p *PeerSamplingFilter) Filter(_ context.Context, nodeID ids.NodeID) bool {
	return p.self != nodeID
}

// NewValidatorSamplingFilter returns an instance of ValidatorSamplingFilter
func NewValidatorSamplingFilter(validatorSet ValidatorSet) *ValidatorSamplingFilter {
	return &ValidatorSamplingFilter{validatorSet: validatorSet}
}

// ValidatorSamplingFilter only samples other validators
type ValidatorSamplingFilter struct {
	validatorSet ValidatorSet
}

func (v *ValidatorSamplingFilter) Filter(ctx context.Context, nodeID ids.NodeID) bool {
	return v.validatorSet.Has(ctx, nodeID)
}
