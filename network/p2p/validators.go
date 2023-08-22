// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"
	"time"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

const maxValidatorSetStaleness = time.Minute

var _ NodeSampler = (*Validators)(nil)

func NewValidators(subnetID ids.ID, validators validators.State) *Validators {
	return &Validators{
		subnetID:                 subnetID,
		validators:               validators,
		maxValidatorSetStaleness: maxValidatorSetStaleness,
	}
}

// Validators contains a set of nodes that are staking.
type Validators struct {
	subnetID   ids.ID
	validators validators.State

	lock                     sync.Mutex
	validatorIDs             []ids.NodeID
	lastUpdated              time.Time
	maxValidatorSetStaleness time.Duration
}

func (v *Validators) refresh(ctx context.Context) {
	if time.Since(v.lastUpdated) < v.maxValidatorSetStaleness {
		return
	}

	height, err := v.validators.GetCurrentHeight(ctx)
	if err != nil {
		return
	}
	validatorSet, err := v.validators.GetValidatorSet(ctx, height, v.subnetID)
	if err != nil {
		return
	}

	v.validatorIDs = maps.Keys(validatorSet)
	v.lastUpdated = time.Now()
}

func (v *Validators) Sample(ctx context.Context, limit int) []ids.NodeID {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.refresh(ctx)

	s := sampler.NewUniform()
	s.Initialize(uint64(len(v.validatorIDs)))

	indices, _ := s.Sample(math.Min(len(v.validatorIDs), limit))
	sampled := make([]ids.NodeID, 0, len(indices))
	for _, i := range indices {
		sampled = append(sampled, v.validatorIDs[i])
	}

	return sampled
}
