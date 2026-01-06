// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// SubnetEVMTestChainID is a subnet-evm specific chain ID for testing
var SubnetEVMTestChainID = ids.GenerateTestID()

// @TODO: This should eventually be replaced by a more robust solution, or alternatively, the presence of nil
// validator states shouldn't be depended upon by tests
func NewTestValidatorState() *validatorstest.State {
	return &validatorstest.State{
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: constants.PrimaryNetworkID,
				snowtest.XChainID:         constants.PrimaryNetworkID,
				snowtest.CChainID:         constants.PrimaryNetworkID,
				SubnetEVMTestChainID:      constants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errors.New("unknown chain")
			}
			return subnetID, nil
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			return nil, nil
		},
		GetWarpValidatorSetF: func(context.Context, uint64, ids.ID) (validators.WarpSet, error) {
			return validators.WarpSet{}, nil
		},
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*validators.GetCurrentValidatorOutput{}, 0, nil
		},
	}
}

// NewTestSnowContext returns a snow.Context with validator state properly configured for testing.
// This wraps snowtest.Context and sets the validator state to avoid the missing GetValidatorSetF issue.
//
// Usage example:
//
//	// Instead of:
//	// snowCtx := utilstest.NewTestSnowContext(t, snowtest.CChainID)
//	// validatorState := utils.NewTestValidatorState()
//	// snowCtx.ValidatorState = validatorState
//
//	// Use:
//	snowCtx := utils.NewTestSnowContext(t)
//
// This function ensures that the snow context has a properly configured validator state
// that includes the GetValidatorSetF function, which is required by many tests.
func NewTestSnowContext(t testing.TB) *snow.Context {
	return NewTestSnowContextWithValidatorState(t, NewTestValidatorState())
}

// NewTestSnowContextWithValidatorState returns a snow.Context with the provided validator state.
// This is useful when you need to customize the validator state behavior for specific tests.
func NewTestSnowContextWithValidatorState(t testing.TB, validatorState validators.State) *snow.Context {
	snowCtx := snowtest.Context(t, SubnetEVMTestChainID)
	snowCtx.ValidatorState = validatorState
	return snowCtx
}
