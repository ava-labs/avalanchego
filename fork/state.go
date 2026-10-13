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
// once it is known. GetCurrentValidatorSet is not overridden: nothing on the
// primary network calls it.
func NewState(inner validators.State, c *Config, forkHeight func() (uint64, bool)) validators.State {
	return &state{
		State:      inner,
		config:     c,
		forkHeight: forkHeight,
	}
}

type state struct {
	validators.State

	config     *Config
	forkHeight func() (uint64, bool)
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
