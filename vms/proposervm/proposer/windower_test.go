// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestWindowerNoValidators(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
			return nil, nil
		},
	}

	r := NewRetriever(vdrState, subnetID, chainID)
	w := New(r)

	r.SetChainHeight(1)
	r.SetPChainHeight(0)
	delay, err := w.Delay(nodeID)
	require.NoError(err)
	require.EqualValues(0, delay)
}

func TestWindowerRepeatedValidator(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	validatorID := ids.GenerateTestNodeID()
	nonValidatorID := ids.GenerateTestNodeID()
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
			return map[ids.NodeID]uint64{
				validatorID: 10,
			}, nil
		},
	}

	r := NewRetriever(vdrState, subnetID, chainID)
	w := New(r)

	r.SetChainHeight(1)
	r.SetPChainHeight(0)
	validatorDelay, err := w.Delay(validatorID)
	require.NoError(err)
	require.EqualValues(0, validatorDelay)

	nonValidatorDelay, err := w.Delay(nonValidatorID)
	require.NoError(err)
	require.EqualValues(MaxDelay, nonValidatorDelay)
}

func TestWindowerChangeByHeight(t *testing.T) {
	require := require.New(t)

	subnetID := ids.ID{0, 1}
	chainID := ids.ID{0, 2}
	validatorIDs := make([]ids.NodeID, MaxWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.NodeID{byte(i + 1)}
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
			validators := make(map[ids.NodeID]uint64, MaxWindows)
			for _, id := range validatorIDs {
				validators[id] = 1
			}
			return validators, nil
		},
	}

	r := NewRetriever(vdrState, subnetID, chainID)
	w := New(r)

	r.SetChainHeight(1)
	r.SetPChainHeight(0)
	expectedDelays1 := []time.Duration{
		2 * WindowDuration,
		5 * WindowDuration,
		3 * WindowDuration,
		4 * WindowDuration,
		0 * WindowDuration,
		1 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w.Delay(vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}

	r.SetChainHeight(2)
	r.SetPChainHeight(0)
	expectedDelays2 := []time.Duration{
		5 * WindowDuration,
		1 * WindowDuration,
		3 * WindowDuration,
		4 * WindowDuration,
		0 * WindowDuration,
		2 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays2 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w.Delay(2, 0, vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}
}

func TestWindowerChangeByChain(t *testing.T) {
	require := require.New(t)

	subnetID := ids.ID{0, 1}

	rand.Seed(0)
	chainID0 := ids.ID{}
	_, _ = rand.Read(chainID0[:]) // #nosec G404
	chainID1 := ids.ID{}
	_, _ = rand.Read(chainID1[:]) // #nosec G404

	validatorIDs := make([]ids.NodeID, MaxWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.NodeID{byte(i + 1)}
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
			validators := make(map[ids.NodeID]uint64, MaxWindows)
			for _, id := range validatorIDs {
				validators[id] = 1
			}
			return validators, nil
		},
	}

	r0 := NewRetriever(vdrState, subnetID, chainID0)
	r1 := NewRetriever(vdrState, subnetID, chainID1)

	w0 := New(r0)
	w1 := New(r1)

	r0.SetChainHeight(2)
	r0.SetPChainHeight(0)
	expectedDelays0 := []time.Duration{
		5 * WindowDuration,
		2 * WindowDuration,
		0 * WindowDuration,
		3 * WindowDuration,
		1 * WindowDuration,
		4 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays0 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w0.Delay(vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}

	r1.SetChainHeight(1)
	r1.SetPChainHeight(0)
	expectedDelays1 := []time.Duration{
		0 * WindowDuration,
		1 * WindowDuration,
		4 * WindowDuration,
		5 * WindowDuration,
		3 * WindowDuration,
		2 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w1.Delay(vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}
}
