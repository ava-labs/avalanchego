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
		slot                   = TimeToSlot(parentBlockTime, blockTime)
	)
	delay, err := w.Delay(context.Background(), chainHeight, pChainHeight, nodeID, MaxVerifyWindows)
	require.NoError(err)
	require.Zero(delay)

	expectedProposer, err := w.ExpectedProposer(context.Background(), chainHeight, pChainHeight, slot)
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
		dummyCtx            = context.Background()
		pChainHeight uint64 = 0
		slot         uint64 = 0
	)

	expectedProposers := map[uint64]ids.NodeID{
		1: validatorIDs[2],
		2: validatorIDs[1],
	}

	for chainHeight, expectedProposerID := range expectedProposers {
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
		require.NoError(err)
		require.Equal(expectedProposerID, proposerID)
	}
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

	var (
		dummyCtx            = context.Background()
		chainHeight  uint64 = 1
		pChainHeight uint64 = 0
		slot         uint64 = 0
	)

	expectedProposers := map[ids.ID]ids.NodeID{
		chainID0: validatorIDs[5],
		chainID1: validatorIDs[3],
	}

	for chainID, expectedProposerID := range expectedProposers {
		w := New(vdrState, subnetID, chainID)
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
		require.NoError(err)
		require.Equal(expectedProposerID, proposerID)
	}
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
		dummyCtx            = context.Background()
		chainHeight  uint64 = 1
		pChainHeight uint64 = 0
	)

	expectedProposers := map[uint64]ids.NodeID{
		0:                 validatorIDs[2],
		1:                 validatorIDs[0],
		2:                 validatorIDs[9],
		MaxLookAheadSlots: validatorIDs[4],
	}

	for slot, expectedProposerID := range expectedProposers {
		actualProposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
		require.NoError(err)
		require.Equal(expectedProposerID, actualProposerID)
	}
}

func TestCoherenceOfExpectedProposerAndMinDelayForProposer(t *testing.T) {
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
		dummyCtx     = context.Background()
		chainHeight  = uint64(1)
		pChainHeight = uint64(0)
	)

	for slot := uint64(0); slot < 3*MaxLookAheadSlots; slot++ {
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
		require.NoError(err)

		// proposerID is the scheduled proposer. It should start with the
		// expected delay
		delay, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, proposerID, slot)
		require.NoError(err)
		require.Equal(time.Duration(slot)*WindowDuration, delay)
	}
}
