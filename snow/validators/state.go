// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
)

const validatorSetsCacheSize = 32

var (
	_ State = (*CachedState)(nil)
	_ State = (*lockedState)(nil)

	ErrValidatorSetForSubnetNotFound = errors.New("validator set for subnet not found")
)

// State allows the lookup of validator sets on specified subnets at the
// requested P-chain height.
type State interface {
	// GetMinimumHeight returns the minimum height of the block still in the
	// proposal window.
	GetMinimumHeight(context.Context) (uint64, error)
	// GetCurrentHeight returns the current height of the P-chain.
	GetCurrentHeight(context.Context) (uint64, error)

	// GetSubnetID returns the subnetID of the provided chain.
	GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error)

	// GetAllValidatorSets returns the validators of all subnets at the
	// requested P-chain height.
	// The returned map should not be modified.
	GetAllValidatorSets(
		ctx context.Context,
		height uint64,
	) (map[ids.ID]map[ids.NodeID]*GetValidatorOutput, error)

	// GetValidatorSet returns the validators of the provided subnet at the
	// requested P-chain height.
	// The returned map should not be modified.
	GetValidatorSet(
		ctx context.Context,
		height uint64,
		subnetID ids.ID,
	) (map[ids.NodeID]*GetValidatorOutput, error)

	// GetCurrentValidatorSet returns the current validators of the provided subnet
	// and the current P-Chain height.
	// Map is keyed by ValidationID.
	GetCurrentValidatorSet(
		ctx context.Context,
		subnetID ids.ID,
	) (map[ids.ID]*GetCurrentValidatorOutput, uint64, error)
}

type lockedState struct {
	lock sync.Locker
	s    State
}

func NewLockedState(lock sync.Locker, s State) State {
	return &lockedState{
		lock: lock,
		s:    s,
	}
}

func (s *lockedState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetMinimumHeight(ctx)
}

func (s *lockedState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetCurrentHeight(ctx)
}

func (s *lockedState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetSubnetID(ctx, chainID)
}

func (s *lockedState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*GetValidatorOutput, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetValidatorSet(ctx, height, subnetID)
}

func (s *lockedState) GetAllValidatorSets(
	ctx context.Context,
	height uint64,
) (map[ids.ID]map[ids.NodeID]*GetValidatorOutput, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetAllValidatorSets(ctx, height)
}

func (s *lockedState) GetCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.ID]*GetCurrentValidatorOutput, uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetCurrentValidatorSet(ctx, subnetID)
}

type noValidators struct {
	State
}

func NewNoValidatorsState(state State) State {
	return &noValidators{
		State: state,
	}
}

func (*noValidators) GetAllValidatorSets(context.Context, uint64) (map[ids.ID]map[ids.NodeID]*GetValidatorOutput, error) {
	return nil, nil
}

func (*noValidators) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*GetValidatorOutput, error) {
	return nil, nil
}

func (n *noValidators) GetCurrentValidatorSet(ctx context.Context, _ ids.ID) (map[ids.ID]*GetCurrentValidatorOutput, uint64, error) {
	height, err := n.GetCurrentHeight(ctx)
	return nil, height, err
}

type CachedState struct {
	State

	// Caches validators for all subnets at various heights.
	// Key: height
	// Value: mapping height -> subnet ID -> validator set
	cache cache.Cacher[uint64, map[ids.ID]map[ids.NodeID]*GetValidatorOutput]
}

func NewCachedState(state State) *CachedState {
	return &CachedState{
		State: state,
		cache: lru.NewCache[uint64, map[ids.ID]map[ids.NodeID]*GetValidatorOutput](validatorSetsCacheSize),
	}
}

func (c *CachedState) GetAllValidatorSets(
	ctx context.Context,
	height uint64,
) (map[ids.ID]map[ids.NodeID]*GetValidatorOutput, error) {
	if validatorSet, ok := c.cache.Get(height); ok {
		return maps.Clone(validatorSet), nil
	}

	validatorSets, err := c.State.GetAllValidatorSets(ctx, height)
	if err != nil {
		return nil, err
	}

	// cache the validator set
	c.cache.Put(height, validatorSets)

	return validatorSets, nil
}

func (c *CachedState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*GetValidatorOutput, error) {
	if validatorSets, ok := c.cache.Get(height); ok {
		validatorSet, exists := validatorSets[subnetID]
		if !exists {
			return nil, ErrValidatorSetForSubnetNotFound
		}
		return maps.Clone(validatorSet), nil
	}

	validatorSets, err := c.GetAllValidatorSets(ctx, height)
	if err != nil {
		return nil, err
	}

	validatorSet, exists := validatorSets[subnetID]
	if !exists {
		return nil, ErrValidatorSetForSubnetNotFound
	}

	return maps.Clone(validatorSet), nil
}
