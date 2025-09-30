// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validatorstest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var (
	errMinimumHeight          = errors.New("unexpectedly called GetMinimumHeight")
	errCurrentHeight          = errors.New("unexpectedly called GetCurrentHeight")
	errSubnetID               = errors.New("unexpectedly called GetSubnetID")
	errGetWarpValidatorSets   = errors.New("unexpectedly called GetWarpValidatorSets")
	errGetWarpValidatorSet    = errors.New("unexpectedly called GetWarpValidatorSet")
	errGetValidatorSet        = errors.New("unexpectedly called GetValidatorSet")
	errGetCurrentValidatorSet = errors.New("unexpectedly called GetCurrentValidatorSet")
)

var _ validators.State = (*State)(nil)

type State struct {
	T testing.TB

	CantGetMinimumHeight,
	CantGetCurrentHeight,
	CantGetSubnetID,
	CantGetWarpValidatorSets bool
	CantGetWarpValidatorSet    bool
	CantGetValidatorSet        bool
	CantGetCurrentValidatorSet bool

	GetMinimumHeightF       func(ctx context.Context) (uint64, error)
	GetCurrentHeightF       func(ctx context.Context) (uint64, error)
	GetSubnetIDF            func(ctx context.Context, chainID ids.ID) (ids.ID, error)
	GetWarpValidatorSetsF   func(ctx context.Context, height uint64) (map[ids.ID]*validators.WarpSet, error)
	GetWarpValidatorSetF    func(ctx context.Context, height uint64, subnetID ids.ID) (*validators.WarpSet, error)
	GetValidatorSetF        func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error)
	GetCurrentValidatorSetF func(ctx context.Context, subnetID ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error)
}

func (vm *State) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if vm.GetMinimumHeightF != nil {
		return vm.GetMinimumHeightF(ctx)
	}
	if vm.CantGetMinimumHeight && vm.T != nil {
		require.FailNow(vm.T, errMinimumHeight.Error())
	}
	return 0, errMinimumHeight
}

func (vm *State) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if vm.GetCurrentHeightF != nil {
		return vm.GetCurrentHeightF(ctx)
	}
	if vm.CantGetCurrentHeight && vm.T != nil {
		require.FailNow(vm.T, errCurrentHeight.Error())
	}
	return 0, errCurrentHeight
}

func (vm *State) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	if vm.GetSubnetIDF != nil {
		return vm.GetSubnetIDF(ctx, chainID)
	}
	if vm.CantGetSubnetID && vm.T != nil {
		require.FailNow(vm.T, errSubnetID.Error())
	}
	return ids.Empty, errSubnetID
}

func (vm *State) GetWarpValidatorSets(
	ctx context.Context,
	height uint64,
) (map[ids.ID]*validators.WarpSet, error) {
	if vm.GetWarpValidatorSetsF != nil {
		return vm.GetWarpValidatorSetsF(ctx, height)
	}
	if vm.CantGetWarpValidatorSets && vm.T != nil {
		require.FailNow(vm.T, errGetWarpValidatorSets.Error())
	}
	return nil, errGetWarpValidatorSets
}

func (vm *State) GetWarpValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (*validators.WarpSet, error) {
	if vm.GetWarpValidatorSetF != nil {
		return vm.GetWarpValidatorSetF(ctx, height, subnetID)
	}
	if vm.CantGetWarpValidatorSet && vm.T != nil {
		require.FailNow(vm.T, errGetWarpValidatorSet.Error())
	}
	return nil, errGetWarpValidatorSet
}

func (vm *State) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	if vm.GetValidatorSetF != nil {
		return vm.GetValidatorSetF(ctx, height, subnetID)
	}
	if vm.CantGetValidatorSet && vm.T != nil {
		require.FailNow(vm.T, errGetValidatorSet.Error())
	}
	return nil, errGetValidatorSet
}

func (vm *State) GetCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	if vm.GetCurrentValidatorSetF != nil {
		return vm.GetCurrentValidatorSetF(ctx, subnetID)
	}
	if vm.CantGetCurrentValidatorSet && vm.T != nil {
		require.FailNow(vm.T, errGetCurrentValidatorSet.Error())
	}
	return nil, 0, errGetCurrentValidatorSet
}
