// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestWindowerNoValidators(t *testing.T) {
	require := require.New(t)

	var (
		subnetID = ids.GenerateTestID()
		chainID  = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)

	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return nil, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	var (
		chainHeight     uint64 = 1
		pChainHeight    uint64 = 0
		parentBlockTime        = time.Now().Truncate(time.Second)
		blockTime              = parentBlockTime.Add(time.Second)
	)
	delay, err := w.Delay(context.Background(), chainHeight, pChainHeight, nodeID, MaxVerifyWindows)
	require.NoError(err)
	require.Zero(delay)

	expectedProposer, err := w.ExpectedProposer(context.Background(), chainHeight, pChainHeight, parentBlockTime, blockTime)
	require.ErrorIs(err, ErrNoProposersAvailable)
	require.Equal(ids.EmptyNodeID, expectedProposer)
}

func TestWindowerRepeatedValidator(t *testing.T) {
	require := require.New(t)

	var (
		subnetID       = ids.GenerateTestID()
		chainID        = ids.GenerateTestID()
		validatorID    = ids.GenerateTestNodeID()
		nonValidatorID = ids.GenerateTestNodeID()
	)

	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{
				validatorID: {
					NodeID: validatorID,
					Weight: 10,
				},
			}, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	validatorDelay, err := w.Delay(context.Background(), 1, 0, validatorID, MaxVerifyWindows)
	require.NoError(err)
	require.Zero(validatorDelay)

	nonValidatorDelay, err := w.Delay(context.Background(), 1, 0, nonValidatorID, MaxVerifyWindows)
	require.NoError(err)
	require.Equal(MaxVerifyDelay, nonValidatorDelay)
}

func TestDelayChangeByHeight(t *testing.T) {
	require := require.New(t)

	var (
		subnetID = ids.ID{0, 1}
		chainID  = ids.ID{0, 2}
	)

	validatorIDs := make([]ids.NodeID, MaxVerifyWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.BuildTestNodeID([]byte{byte(i) + 1})
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, MaxVerifyWindows)
			for _, id := range validatorIDs {
				vdrs[id] = &validators.GetValidatorOutput{
					NodeID: id,
					Weight: 1,
				}
			}
			return vdrs, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

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
		validatorDelay, err := w.Delay(context.Background(), 1, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}

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
		validatorDelay, err := w.Delay(context.Background(), 2, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}
}

func TestDelayChangeByChain(t *testing.T) {
	require := require.New(t)

	subnetID := ids.ID{0, 1}

	source := rand.NewSource(int64(0))
	rng := rand.New(source) // #nosec G404

	chainID0 := ids.ID{}
	_, err := rng.Read(chainID0[:])
	require.NoError(err)

	chainID1 := ids.ID{}
	_, err = rng.Read(chainID1[:])
	require.NoError(err)

	validatorIDs := make([]ids.NodeID, MaxVerifyWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.BuildTestNodeID([]byte{byte(i) + 1})
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, MaxVerifyWindows)
			for _, id := range validatorIDs {
				vdrs[id] = &validators.GetValidatorOutput{
					NodeID: id,
					Weight: 1,
				}
			}
			return vdrs, nil
		},
	}

	w0 := New(vdrState, subnetID, chainID0)
	w1 := New(vdrState, subnetID, chainID1)

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
		validatorDelay, err := w0.Delay(context.Background(), 1, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}

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
		validatorDelay, err := w1.Delay(context.Background(), 1, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}
}

func TestExpectedProposerChangeByHeight(t *testing.T) {
	require := require.New(t)

	var (
		subnetID = ids.ID{0, 1}
		chainID  = ids.ID{0, 2}

		validatorsCount = 10
	)

	validatorIDs := make([]ids.NodeID, validatorsCount)
	for i := range validatorIDs {
		validatorIDs[i] = ids.BuildTestNodeID([]byte{byte(i) + 1})
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, MaxVerifyWindows)
			for _, id := range validatorIDs {
				vdrs[id] = &validators.GetValidatorOutput{
					NodeID: id,
					Weight: 1,
				}
			}
			return vdrs, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	var (
		dummyCtx        = context.Background()
		chainHeight     = uint64(1)
		pChainHeight    = uint64(0)
		parentBlockTime = time.Now().Truncate(time.Second)
		blockTime       = parentBlockTime.Add(time.Second)
	)

	proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
	require.NoError(err)
	require.Equal(validatorIDs[2], proposerID)

	chainHeight = 2
	proposerID, err = w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
	require.NoError(err)
	require.Equal(validatorIDs[1], proposerID)
}

func TestExpectedProposerChangeByChain(t *testing.T) {
	require := require.New(t)

	var (
		subnetID        = ids.ID{0, 1}
		validatorsCount = 10
	)

	source := rand.NewSource(int64(0))
	rng := rand.New(source) // #nosec G404

	chainID0 := ids.ID{}
	_, err := rng.Read(chainID0[:])
	require.NoError(err)

	chainID1 := ids.ID{}
	_, err = rng.Read(chainID1[:])
	require.NoError(err)

	validatorIDs := make([]ids.NodeID, validatorsCount)
	for i := range validatorIDs {
		validatorIDs[i] = ids.BuildTestNodeID([]byte{byte(i) + 1})
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, MaxVerifyWindows)
			for _, id := range validatorIDs {
				vdrs[id] = &validators.GetValidatorOutput{
					NodeID: id,
					Weight: 1,
				}
			}
			return vdrs, nil
		},
	}

	w0 := New(vdrState, subnetID, chainID0)
	w1 := New(vdrState, subnetID, chainID1)

	var (
		dummyCtx        = context.Background()
		chainHeight     = uint64(1)
		pChainHeight    = uint64(0)
		parentBlockTime = time.Now().Truncate(time.Second)
		blockTime       = parentBlockTime.Add(time.Second)
	)

	proposerID, err := w0.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
	require.NoError(err)
	require.Equal(validatorIDs[5], proposerID)

	proposerID, err = w1.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
	require.NoError(err)
	require.Equal(validatorIDs[3], proposerID)
}

func TestExpectedProposerChangeBySlot(t *testing.T) {
	require := require.New(t)

	var (
		subnetID = ids.ID{0, 1}
		chainID  = ids.ID{0, 2}

		validatorsCount = 10
	)

	validatorIDs := make([]ids.NodeID, validatorsCount)
	for i := range validatorIDs {
		validatorIDs[i] = ids.BuildTestNodeID([]byte{byte(i) + 1})
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, MaxVerifyWindows)
			for _, id := range validatorIDs {
				vdrs[id] = &validators.GetValidatorOutput{
					NodeID: id,
					Weight: 1,
				}
			}
			return vdrs, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	var (
		dummyCtx        = context.Background()
		chainHeight     = uint64(1)
		pChainHeight    = uint64(0)
		parentBlockTime = time.Now().Truncate(time.Second)
		blockTime       = parentBlockTime.Add(time.Second)
	)

	{
		// base case. Next tests are variations on top of this.
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
		require.NoError(err)
		require.Equal(validatorIDs[2], proposerID)

		// proposerID is the scheduled proposer. It may start with no further delay
		delay, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, proposerID, blockTime)
		require.NoError(err)
		require.Zero(delay)
	}

	{
		// proposerID won't change within the same slot
		blockTime = parentBlockTime.Add(WindowDuration).Add(-1 * time.Second)
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
		require.NoError(err)
		require.Equal(validatorIDs[2], proposerID)

		// proposerID is the scheduled proposer. It may start with no further delay
		delay, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, proposerID, blockTime)
		require.NoError(err)
		require.Zero(delay)
	}

	{
		// proposerID changes with new slot
		blockTime = parentBlockTime.Add(WindowDuration)
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
		require.NoError(err)
		require.Equal(validatorIDs[0], proposerID)

		// proposerID is the scheduled proposer. It may start with no further delay
		delay, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, proposerID, blockTime)
		require.NoError(err)
		require.Zero(delay)
	}

	{
		// proposerID changes with new slot
		blockTime = parentBlockTime.Add(2 * WindowDuration)
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, blockTime)
		require.NoError(err)
		require.Equal(validatorIDs[9], proposerID)

		// proposerID is the scheduled proposer. It may start with no further delay
		delay, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, parentBlockTime, proposerID, blockTime)
		require.NoError(err)
		require.Zero(delay)
	}
}
