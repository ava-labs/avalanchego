// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

var _ State = (*lockedState)(nil)

// State allows the lookup of validator sets on specified subnets at the
// requested P-chain height.
type State interface {
	// GetMinimumHeight returns the minimum height of the block still in the
	// proposal window.
	GetMinimumHeight() (uint64, error)
	// GetCurrentHeight returns the current height of the P-chain.
	GetCurrentHeight() (uint64, error)

	// GetValidatorSet returns the weights of the nodeIDs for the provided
	// subnet at the requested P-chain height.
	// The returned map should not be modified.
	GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error)
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

func (s *lockedState) GetMinimumHeight() (uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetMinimumHeight()
}

func (s *lockedState) GetCurrentHeight() (uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetCurrentHeight()
}

func (s *lockedState) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetValidatorSet(height, subnetID)
}

type noValidators struct {
	State
}

func NewNoValidatorsState(state State) State {
	return &noValidators{
		State: state,
	}
}

func (*noValidators) GetValidatorSet(uint64, ids.ID) (map[ids.NodeID]uint64, error) {
	return nil, nil
}
