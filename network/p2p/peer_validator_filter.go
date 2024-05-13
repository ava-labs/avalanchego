// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// PeerValidatorFilter only samples other validators
type PeerValidatorFilter struct {
	self         ids.NodeID
	validatorSet ValidatorSet
}

func (p *PeerValidatorFilter) Filter(
	ctx context.Context,
	nodeID ids.NodeID,
) bool {
	return nodeID != p.self && p.validatorSet.Has(ctx, nodeID)
}

func NewPeerValidatorFilter(
	self ids.NodeID,
	validatorSet ValidatorSet,
) *PeerValidatorFilter {
	return &PeerValidatorFilter{
		self:         self,
		validatorSet: validatorSet,
	}
}
