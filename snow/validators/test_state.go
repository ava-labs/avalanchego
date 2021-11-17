// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errCurrentHeight   = errors.New("unexpectedly called GetCurrentHeight")
	errGetValidatorSet = errors.New("unexpectedly called GetValidatorSet")
)

type TestState struct {
	T *testing.T

	CantGetCurrentHeight,
	CantGetValidatorSet bool

	GetCurrentHeightF func() (uint64, error)
	GetValidatorSetF  func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error)
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
