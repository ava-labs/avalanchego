// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errMinimumHeight   = errors.New("unexpectedly called GetMinimumHeight")
	errCurrentHeight   = errors.New("unexpectedly called GetCurrentHeight")
	errSubnetID        = errors.New("unexpectedly called GetSubnetID")
	errGetValidatorSet = errors.New("unexpectedly called GetValidatorSet")
)

var _ State = (*TestState)(nil)

type TestState struct {
	T *testing.T

	CantGetMinimumHeight,
	CantGetCurrentHeight,
	CantGetSubnetID,
	CantGetValidatorSet bool

	GetMinimumHeightF func(ctx context.Context) (uint64, error)
	GetCurrentHeightF func(ctx context.Context) (uint64, error)
	GetSubnetIDF      func(ctx context.Context, chainID ids.ID) (ids.ID, error)
	GetValidatorSetF  func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*GetValidatorOutput, error)
}

func (vm *TestState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if vm.GetMinimumHeightF != nil {
		return vm.GetMinimumHeightF(ctx)
	}
	if vm.CantGetMinimumHeight && vm.T != nil {
		require.FailNow(vm.T, errMinimumHeight.Error())
	}
	return 0, errMinimumHeight
}

func (vm *TestState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if vm.GetCurrentHeightF != nil {
		return vm.GetCurrentHeightF(ctx)
	}
	if vm.CantGetCurrentHeight && vm.T != nil {
		require.FailNow(vm.T, errCurrentHeight.Error())
	}
	return 0, errCurrentHeight
}

func (vm *TestState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	if vm.GetSubnetIDF != nil {
		return vm.GetSubnetIDF(ctx, chainID)
	}
	if vm.CantGetSubnetID && vm.T != nil {
		require.FailNow(vm.T, errSubnetID.Error())
	}
	return ids.Empty, errSubnetID
}

func (vm *TestState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*GetValidatorOutput, error) {
	if vm.GetValidatorSetF != nil {
		return vm.GetValidatorSetF(ctx, height, subnetID)
	}
	if vm.CantGetValidatorSet && vm.T != nil {
		require.FailNow(vm.T, errGetValidatorSet.Error())
	}
	return nil, errGetValidatorSet
}
