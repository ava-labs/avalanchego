// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

const (
	maxValidatorSetStaleness = time.Minute
)

var (
	_ NodeSampler = (*Validators)(nil)

	_ validators.Connector = (*Peers)(nil)
	_ NodeSampler          = (*Peers)(nil)
)

type NodeSampler interface {
	// Sample returns at most [n] nodes. This may return fewer nodes if fewer
	// than [n] are available.
	Sample(n int) ([]ids.NodeID, error)
}

func NewValidators(subnetID ids.ID, validators validators.State) *Validators {
	return &Validators{
		subnetID:                 subnetID,
		validators:               validators,
		maxValidatorSetStaleness: maxValidatorSetStaleness,
	}
}

type Validators struct {
	subnetID   ids.ID
	validators validators.State

	recentValidatorIDs       []ids.NodeID
	lastUpdated              time.Time
	maxValidatorSetStaleness time.Duration
}

func (v *Validators) getRecentValidatorIDs() ([]ids.NodeID, error) {
	if time.Since(v.lastUpdated) < v.maxValidatorSetStaleness {
		return v.recentValidatorIDs, nil
	}

	height, err := v.validators.GetCurrentHeight(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to get current height: %w", err)
	}

	validatorSet, err := v.validators.GetValidatorSet(context.TODO(), height, v.subnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current validator set: %w", err)
	}

	v.recentValidatorIDs = maps.Keys(validatorSet)
	return v.recentValidatorIDs, nil
}

func (v *Validators) Sample(n int) ([]ids.NodeID, error) {
	validatorIDs, err := v.getRecentValidatorIDs()
	if err != nil {
		return nil, err
	}

	s := sampler.NewUniform()
	s.Initialize(uint64(len(validatorIDs)))

	indices, _ := s.Sample(safemath.Min(len(validatorIDs), n))
	sampled := make([]ids.NodeID, 0, len(indices))
	for _, i := range indices {
		sampled = append(sampled, validatorIDs[i])
	}

	return sampled, nil
}

type Peers struct {
	lock  sync.RWMutex
	peers set.SampleableSet[ids.NodeID]
}

func (p *Peers) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.Add(nodeID)

	return nil
}

func (p *Peers) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.Remove(nodeID)

	return nil
}

func (p *Peers) Sample(n int) ([]ids.NodeID, error) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.Sample(n), nil
}
