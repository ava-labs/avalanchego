// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork

import (
	"context"
	"errors"
	"maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	_ validators.State = (*staticState)(nil)
	_ validators.State = (*state)(nil)

	errStaticState = errors.New("not supported by the static fork validator state")
)

// NewStaticState returns a validators.State whose primary network validator
// set is the fork set at every height. It only backs the fork proposer
// windower, which uses GetValidatorSet alone.
func NewStaticState(c *Config) validators.State {
	return &staticState{vdrs: c.ValidatorSet()}
}

type staticState struct {
	vdrs map[ids.NodeID]*validators.GetValidatorOutput
}

func (*staticState) GetMinimumHeight(context.Context) (uint64, error) {
	return 0, errStaticState
}

func (*staticState) GetCurrentHeight(context.Context) (uint64, error) {
	return 0, errStaticState
}

func (*staticState) GetSubnetID(context.Context, ids.ID) (ids.ID, error) {
	return ids.Empty, errStaticState
}

func (*staticState) GetWarpValidatorSets(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
	return nil, errStaticState
}

func (s *staticState) GetValidatorSet(_ context.Context, _ uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	if subnetID != constants.PrimaryNetworkID {
		return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
	}
	return s.vdrs, nil
}

func (*staticState) GetCurrentValidatorSet(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	return nil, 0, errStaticState
}

// NewState wraps [inner] so that primary network validator lookups at P-chain
// heights at or above H_fork return the fork set. [forkHeight] reports H_fork
// once it is known.
func NewState(inner validators.State, c *Config, forkHeight func() (uint64, bool)) validators.State {
	current := make(map[ids.ID]*validators.GetCurrentValidatorOutput, len(c.Validators))
	for _, v := range c.Validators {
		// Fork validators have no staking transaction; derive a stable,
		// unique validation ID from the node ID.
		validationID := ids.ID(hashing.ComputeHash256Array(v.NodeID.Bytes()))
		current[validationID] = &validators.GetCurrentValidatorOutput{
			ValidationID: validationID,
			NodeID:       v.NodeID,
			PublicKey:    v.Signer.Key(),
			Weight:       v.Weight,
			IsActive:     true,
		}
	}
	return &state{
		State:      inner,
		config:     c,
		forkHeight: forkHeight,
		current:    current,
	}
}

type state struct {
	validators.State

	config     *Config
	forkHeight func() (uint64, bool)
	current    map[ids.ID]*validators.GetCurrentValidatorOutput
}

func (s *state) isForked(height uint64) bool {
	forkHeight, ok := s.forkHeight()
	return ok && height >= forkHeight
}

func (s *state) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	if subnetID == constants.PrimaryNetworkID && s.isForked(height) {
		return s.config.ValidatorSet(), nil
	}
	return s.State.GetValidatorSet(ctx, height, subnetID)
}

func (s *state) GetWarpValidatorSets(ctx context.Context, height uint64) (map[ids.ID]validators.WarpSet, error) {
	sets, err := s.State.GetWarpValidatorSets(ctx, height)
	if err != nil || !s.isForked(height) {
		return sets, err
	}
	forked := maps.Clone(sets)
	if forked == nil {
		forked = make(map[ids.ID]validators.WarpSet, 1)
	}
	forked[constants.PrimaryNetworkID] = s.config.WarpSet()
	return forked, nil
}

func (s *state) GetCurrentValidatorSet(ctx context.Context, subnetID ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	vdrs, height, err := s.State.GetCurrentValidatorSet(ctx, subnetID)
	if err != nil || subnetID != constants.PrimaryNetworkID || !s.isForked(height) {
		return vdrs, height, err
	}
	return s.current, height, nil
}
