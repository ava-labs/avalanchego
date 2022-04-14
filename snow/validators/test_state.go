// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errMinimumHeight   = errors.New("unexpectedly called GetMinimumHeight")
	errCurrentHeight   = errors.New("unexpectedly called GetCurrentHeight")
	errGetValidatorSet = errors.New("unexpectedly called GetValidatorSet")
)

var _ State = &TestState{}

type TestState struct {
	T *testing.T

	CantGetMinimumHeight,
	CantGetCurrentHeight,
	CantGetValidatorSet bool

	GetMinimumHeightF func() (uint64, error)
	GetCurrentHeightF func() (uint64, error)
	GetValidatorSetF  func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error)
}

func (vm *TestState) GetMinimumHeight() (uint64, error) {
	if vm.GetMinimumHeightF != nil {
		return vm.GetMinimumHeightF()
	}
	if vm.CantGetMinimumHeight && vm.T != nil {
		vm.T.Fatal(errMinimumHeight)
	}
	return 0, errMinimumHeight
}

func (vm *TestState) GetCurrentHeight() (uint64, error) {
	if vm.GetCurrentHeightF != nil {
		return vm.GetCurrentHeightF()
	}
	if vm.CantGetCurrentHeight && vm.T != nil {
		vm.T.Fatal(errCurrentHeight)
	}
	return 0, errCurrentHeight
}

func (vm *TestState) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
	if vm.GetValidatorSetF != nil {
		return vm.GetValidatorSetF(height, subnetID)
	}
	if vm.CantGetValidatorSet && vm.T != nil {
		vm.T.Fatal(errGetValidatorSet)
	}
	return nil, errGetValidatorSet
}
