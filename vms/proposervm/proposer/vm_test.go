// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var (
	errNoValidators = errors.New("no validators")

	_ validators.VM = &testVM{}
)

type testVM struct {
	currentHeight    uint64
	currentHeightErr error
	getValidatorSetF func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error)
}

func (vm *testVM) GetCurrentHeight() (uint64, error) {
	return vm.currentHeight, vm.currentHeightErr
}

func (vm *testVM) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
	if vm.getValidatorSetF == nil {
		return nil, errNoValidators
	}
	return vm.getValidatorSetF(height, subnetID)
}
