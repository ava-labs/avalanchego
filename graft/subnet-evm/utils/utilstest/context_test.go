// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestNewTestSnowContext(t *testing.T) {
	// Test that NewTestSnowContext creates a context with validator state
	snowCtx := NewTestSnowContext(t)
	require.NotNil(t, snowCtx.ValidatorState)

	// Test that the validator state has the required functions
	validatorState := snowCtx.ValidatorState
	require.NotNil(t, validatorState)

	// Test that we can call GetValidatorSetF without panicking
	validators, err := validatorState.GetValidatorSet(t.Context(), 0, ids.Empty)
	require.NoError(t, err)
	require.NotNil(t, validators)

	// Test that we can call GetWarpValidatorSetF without panicking
	_, err = validatorState.GetWarpValidatorSet(t.Context(), 0, ids.Empty)
	require.NoError(t, err)

	// Test that we can call GetWarpValidatorSetsF without panicking
	_, err = validatorState.GetWarpValidatorSets(t.Context(), 0)
	require.NoError(t, err)

	// Test that we can call GetCurrentValidatorSetF without panicking
	currentValidators, height, err := validatorState.GetCurrentValidatorSet(t.Context(), ids.Empty)
	require.NoError(t, err)
	require.NotNil(t, currentValidators)
	require.Equal(t, uint64(0), height)
}
