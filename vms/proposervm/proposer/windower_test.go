// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"context"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/logging"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	subnetID      = ids.GenerateTestID()
	randomChainID = ids.GenerateTestID()
	fixedChainID  = ids.ID{0, 2}
)

func TestWindowerNoValidators(t *testing.T) {
	tests := []struct {
		name       string
		validators []ids.NodeID
	}{
		{
			name: "no validators",
		},
		{
			name: "only inactive validators",
			validators: []ids.NodeID{
				ids.EmptyNodeID,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			w := New(
				makeValidatorState(t, test.validators),
				subnetID,
				randomChainID,
				DefaultWindowDuration,
				&logging.NoLog{},
			)

			var (
				chainHeight  uint64 = 1
				pChainHeight uint64 = 0
				nodeID              = ids.GenerateTestNodeID()
				slot         uint64 = 1
			)
			delay, err := w.Delay(t.Context(), chainHeight, pChainHeight, nodeID, MaxVerifyWindows)
			require.NoError(err)
			require.Zero(delay)

			proposer, err := w.ExpectedProposer(t.Context(), chainHeight, pChainHeight, slot)
			require.ErrorIs(err, ErrAnyoneCanPropose)
			require.Equal(ids.EmptyNodeID, proposer)

			delay, err = w.MinDelayForProposer(t.Context(), chainHeight, pChainHeight, nodeID, slot)
			require.ErrorIs(err, ErrAnyoneCanPropose)
			require.Zero(delay)
		})
	}
}

func TestWindowerRepeatedValidator(t *testing.T) {
	require := require.New(t)

	var (
		validatorID    = ids.GenerateTestNodeID()
		nonValidatorID = ids.GenerateTestNodeID()
	)

	vdrState := &validatorstest.State{
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

	w := New(vdrState, subnetID, randomChainID, DefaultWindowDuration, &logging.NoLog{})

	validatorDelay, err := w.Delay(t.Context(), 1, 0, validatorID, MaxVerifyWindows)
	require.NoError(err)
	require.Zero(validatorDelay)

	nonValidatorDelay, err := w.Delay(t.Context(), 1, 0, nonValidatorID, MaxVerifyWindows)
	require.NoError(err)
	require.Equal(DefaultWindowDuration.MaxVerifyDelay(), nonValidatorDelay)
}

func TestDelayChangeByHeight(t *testing.T) {
	require := require.New(t)

	validatorIDs, vdrState := makeValidators(t, MaxVerifyWindows)
	w := New(vdrState, subnetID, fixedChainID, DefaultWindowDuration, &logging.NoLog{})

	expectedDelays1 := []time.Duration{
		2 * time.Duration(DefaultWindowDuration),
		5 * time.Duration(DefaultWindowDuration),
		3 * time.Duration(DefaultWindowDuration),
		4 * time.Duration(DefaultWindowDuration),
		0 * time.Duration(DefaultWindowDuration),
		1 * time.Duration(DefaultWindowDuration),
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w.Delay(t.Context(), 1, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}

	expectedDelays2 := []time.Duration{
		5 * time.Duration(DefaultWindowDuration),
		1 * time.Duration(DefaultWindowDuration),
		3 * time.Duration(DefaultWindowDuration),
		4 * time.Duration(DefaultWindowDuration),
		0 * time.Duration(DefaultWindowDuration),
		2 * time.Duration(DefaultWindowDuration),
	}
	for i, expectedDelay := range expectedDelays2 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w.Delay(t.Context(), 2, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}
}

func TestDelayChangeByChain(t *testing.T) {
	require := require.New(t)

	source := rand.NewSource(int64(0))
	rng := rand.New(source)

	chainID0 := ids.Empty
	_, err := rng.Read(chainID0[:])
	require.NoError(err)

	chainID1 := ids.Empty
	_, err = rng.Read(chainID1[:])
	require.NoError(err)

	validatorIDs, vdrState := makeValidators(t, MaxVerifyWindows)
	w0 := New(vdrState, subnetID, chainID0, DefaultWindowDuration, &logging.NoLog{})
	w1 := New(vdrState, subnetID, chainID1, DefaultWindowDuration, &logging.NoLog{})

	expectedDelays0 := []time.Duration{
		5 * time.Duration(DefaultWindowDuration),
		2 * time.Duration(DefaultWindowDuration),
		0 * time.Duration(DefaultWindowDuration),
		3 * time.Duration(DefaultWindowDuration),
		1 * time.Duration(DefaultWindowDuration),
		4 * time.Duration(DefaultWindowDuration),
	}
	for i, expectedDelay := range expectedDelays0 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w0.Delay(t.Context(), 1, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}

	expectedDelays1 := []time.Duration{
		0 * time.Duration(DefaultWindowDuration),
		1 * time.Duration(DefaultWindowDuration),
		4 * time.Duration(DefaultWindowDuration),
		5 * time.Duration(DefaultWindowDuration),
		3 * time.Duration(DefaultWindowDuration),
		2 * time.Duration(DefaultWindowDuration),
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w1.Delay(t.Context(), 1, 0, vdrID, MaxVerifyWindows)
		require.NoError(err)
		require.Equal(expectedDelay, validatorDelay)
	}
}

func TestExpectedProposerChangeByHeight(t *testing.T) {
	require := require.New(t)

	validatorIDs, vdrState := makeValidators(t, 10)
	w := New(vdrState, subnetID, fixedChainID, DefaultWindowDuration, &logging.NoLog{})

	var (
		dummyCtx            = t.Context()
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

	source := rand.NewSource(int64(0))
	rng := rand.New(source)

	chainID0 := ids.Empty
	_, err := rng.Read(chainID0[:])
	require.NoError(err)

	chainID1 := ids.Empty
	_, err = rng.Read(chainID1[:])
	require.NoError(err)

	validatorIDs, vdrState := makeValidators(t, 10)

	var (
		dummyCtx            = t.Context()
		chainHeight  uint64 = 1
		pChainHeight uint64 = 0
		slot         uint64 = 0
	)

	expectedProposers := map[ids.ID]ids.NodeID{
		chainID0: validatorIDs[5],
		chainID1: validatorIDs[3],
	}

	for chainID, expectedProposerID := range expectedProposers {
		w := New(vdrState, subnetID, chainID, DefaultWindowDuration, &logging.NoLog{})
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
		require.NoError(err)
		require.Equal(expectedProposerID, proposerID)
	}
}

func TestExpectedProposerChangeBySlot(t *testing.T) {
	require := require.New(t)

	validatorIDs, vdrState := makeValidators(t, 10)
	w := New(vdrState, subnetID, fixedChainID, DefaultWindowDuration, &logging.NoLog{})

	var (
		dummyCtx            = t.Context()
		chainHeight  uint64 = 1
		pChainHeight uint64 = 0
	)

	proposers := []ids.NodeID{
		validatorIDs[2],
		validatorIDs[0],
		validatorIDs[9],
		validatorIDs[7],
		validatorIDs[0],
		validatorIDs[3],
		validatorIDs[3],
		validatorIDs[3],
		validatorIDs[3],
		validatorIDs[3],
		validatorIDs[4],
		validatorIDs[0],
		validatorIDs[6],
		validatorIDs[3],
		validatorIDs[2],
		validatorIDs[1],
		validatorIDs[6],
		validatorIDs[0],
		validatorIDs[5],
		validatorIDs[1],
		validatorIDs[9],
		validatorIDs[6],
		validatorIDs[0],
		validatorIDs[8],
	}
	expectedProposers := map[uint64]ids.NodeID{
		MaxLookAheadSlots:     validatorIDs[4],
		MaxLookAheadSlots + 1: validatorIDs[6],
	}
	for slot, expectedProposerID := range proposers {
		expectedProposers[uint64(slot)] = expectedProposerID
	}

	for slot, expectedProposerID := range expectedProposers {
		actualProposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
		require.NoError(err)
		require.Equal(expectedProposerID, actualProposerID)
	}
}

func TestCoherenceOfExpectedProposerAndMinDelayForProposer(t *testing.T) {
	require := require.New(t)

	_, vdrState := makeValidators(t, 10)
	w := New(vdrState, subnetID, fixedChainID, DefaultWindowDuration, &logging.NoLog{})

	var (
		dummyCtx            = t.Context()
		chainHeight  uint64 = 1
		pChainHeight uint64 = 0
	)

	for slot := uint64(0); slot < 3*MaxLookAheadSlots; slot++ {
		proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
		require.NoError(err)

		// proposerID is the scheduled proposer. It should start with the
		// expected delay
		delay, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, proposerID, slot)
		require.NoError(err)
		require.Equal(time.Duration(slot)*time.Duration(DefaultWindowDuration), delay)
	}
}

func TestMinDelayForProposer(t *testing.T) {
	require := require.New(t)

	validatorIDs, vdrState := makeValidators(t, 10)
	w := New(vdrState, subnetID, fixedChainID, DefaultWindowDuration, &logging.NoLog{})

	var (
		dummyCtx            = t.Context()
		chainHeight  uint64 = 1
		pChainHeight uint64 = 0
		slot         uint64 = 0
	)

	expectedDelays := map[ids.NodeID]time.Duration{
		validatorIDs[0]:          1 * time.Duration(DefaultWindowDuration),
		validatorIDs[1]:          15 * time.Duration(DefaultWindowDuration),
		validatorIDs[2]:          0 * time.Duration(DefaultWindowDuration),
		validatorIDs[3]:          5 * time.Duration(DefaultWindowDuration),
		validatorIDs[4]:          10 * time.Duration(DefaultWindowDuration),
		validatorIDs[5]:          18 * time.Duration(DefaultWindowDuration),
		validatorIDs[6]:          12 * time.Duration(DefaultWindowDuration),
		validatorIDs[7]:          3 * time.Duration(DefaultWindowDuration),
		validatorIDs[8]:          23 * time.Duration(DefaultWindowDuration),
		validatorIDs[9]:          2 * time.Duration(DefaultWindowDuration),
		ids.GenerateTestNodeID(): MaxLookAheadSlots * time.Duration(DefaultWindowDuration),
	}

	for nodeID, expectedDelay := range expectedDelays {
		delay, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, nodeID, slot)
		require.NoError(err)
		require.Equal(expectedDelay, delay)
	}
}

func TestWindowDurationOf(t *testing.T) {
	tests := []struct {
		name               string
		upgrades           upgrade.Config
		want               WindowDuration
		wantMaxVerifyDelay time.Duration
		wantMaxBuildDelay  time.Duration
		wantErr            error
	}{
		{
			name:               "mainnet",
			upgrades:           upgrade.Mainnet,
			want:               WindowDuration(5 * time.Second),
			wantMaxVerifyDelay: 30 * time.Second,
			wantMaxBuildDelay:  5 * time.Minute,
		},
		{
			name:               "fuji",
			upgrades:           upgrade.Fuji,
			want:               WindowDuration(5 * time.Second),
			wantMaxVerifyDelay: 30 * time.Second,
			wantMaxBuildDelay:  5 * time.Minute,
		},
		{
			name:               "default",
			upgrades:           upgrade.Default,
			want:               WindowDuration(5 * time.Second),
			wantMaxVerifyDelay: 30 * time.Second,
			wantMaxBuildDelay:  5 * time.Minute,
		},
		{
			name:               "one_second",
			upgrades:           upgrade.Config{ProposerWindowDuration: time.Second},
			want:               WindowDuration(time.Second),
			wantMaxVerifyDelay: 6 * time.Second,
			wantMaxBuildDelay:  time.Minute,
		},
		{
			name:     "under_one_second",
			upgrades: upgrade.Config{ProposerWindowDuration: time.Second - time.Millisecond},
			wantErr:  ErrWindowDurationTooShort,
		},
		{
			name:     "negative",
			upgrades: upgrade.Config{ProposerWindowDuration: -time.Second},
			wantErr:  ErrWindowDurationTooShort,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := WindowDurationOf(test.upgrades)
			require.ErrorIs(t, err, test.wantErr)
			if test.wantErr != nil {
				return
			}

			require.Equal(t, test.want, got)
			require.Equal(t, test.wantMaxVerifyDelay, got.MaxVerifyDelay())
			require.Equal(t, test.wantMaxBuildDelay, got.MaxBuildDelay())
		})
	}
}

func TestOneSecondWindowDelays(t *testing.T) {
	window, err := WindowDurationOf(upgrade.Config{ProposerWindowDuration: time.Second})
	require.NoError(t, err)

	validatorIDs, vdrState := makeValidators(t, 10)
	w := New(vdrState, subnetID, fixedChainID, window, &logging.NoLog{})

	var (
		chainHeight  uint64 = 1
		pChainHeight uint64 = 0
	)

	// The slots of TestMinDelayForProposer, one second each.
	wantMinDelays := map[ids.NodeID]time.Duration{
		validatorIDs[0]:          time.Second,
		validatorIDs[1]:          15 * time.Second,
		validatorIDs[2]:          0,
		validatorIDs[9]:          2 * time.Second,
		ids.GenerateTestNodeID(): 12 * time.Minute, // MaxLookAheadSlots
	}
	for nodeID, want := range wantMinDelays {
		got, err := w.MinDelayForProposer(t.Context(), chainHeight, pChainHeight, nodeID, 0)
		require.NoError(t, err)
		require.Equal(t, want, got, "MinDelayForProposer(%s)", nodeID)
	}

	nonValidatorDelay, err := w.Delay(t.Context(), chainHeight, pChainHeight, ids.GenerateTestNodeID(), MaxVerifyWindows)
	require.NoError(t, err)
	require.Equal(t, 6*time.Second, nonValidatorDelay)

	parentTime := time.Unix(1_000_000, 0)
	require.Equal(t, uint64(3), window.TimeToSlot(parentTime, parentTime.Add(3*time.Second)))
}

func BenchmarkMinDelayForProposer(b *testing.B) {
	require := require.New(b)

	_, vdrState := makeValidators(b, 10)
	w := New(vdrState, subnetID, fixedChainID, DefaultWindowDuration, &logging.NoLog{})

	var (
		dummyCtx            = b.Context()
		pChainHeight uint64 = 0
		chainHeight  uint64 = 1
		nodeID              = ids.GenerateTestNodeID() // Ensure to exhaust the search
		slot         uint64 = 0
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := w.MinDelayForProposer(dummyCtx, chainHeight, pChainHeight, nodeID, slot)
		require.NoError(err)
	}
}

func TestTimeToSlot(t *testing.T) {
	parentTime := time.Now()
	tests := []struct {
		timeOffset   time.Duration
		expectedSlot uint64
	}{
		{
			timeOffset:   -time.Duration(DefaultWindowDuration),
			expectedSlot: 0,
		},
		{
			timeOffset:   -time.Second,
			expectedSlot: 0,
		},
		{
			timeOffset:   0,
			expectedSlot: 0,
		},
		{
			timeOffset:   time.Duration(DefaultWindowDuration),
			expectedSlot: 1,
		},
		{
			timeOffset:   2 * time.Duration(DefaultWindowDuration),
			expectedSlot: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.timeOffset.String(), func(t *testing.T) {
			slot := DefaultWindowDuration.TimeToSlot(parentTime, parentTime.Add(test.timeOffset))
			require.Equal(t, test.expectedSlot, slot)
		})
	}
}

// Ensure that the proposer distribution is within 3 standard deviations of the
// expected value assuming a truly random binomial distribution.
func TestProposerDistribution(t *testing.T) {
	require := require.New(t)

	validatorIDs, vdrState := makeValidators(t, 10)
	w := New(vdrState, subnetID, fixedChainID, DefaultWindowDuration, &logging.NoLog{})

	var (
		dummyCtx               = t.Context()
		pChainHeight    uint64 = 0
		numChainHeights uint64 = 100
		numSlots        uint64 = 100
	)

	proposerFrequency := make(map[ids.NodeID]int)
	for _, validatorID := range validatorIDs {
		// Initialize the map to 0s to include validators that are never sampled
		// in the analysis.
		proposerFrequency[validatorID] = 0
	}
	for chainHeight := uint64(0); chainHeight < numChainHeights; chainHeight++ {
		for slot := uint64(0); slot < numSlots; slot++ {
			proposerID, err := w.ExpectedProposer(dummyCtx, chainHeight, pChainHeight, slot)
			require.NoError(err)
			proposerFrequency[proposerID]++
		}
	}

	var (
		totalNumberOfSamples      = numChainHeights * numSlots
		probabilityOfBeingSampled = 1 / float64(len(validatorIDs))
		expectedNumberOfSamples   = uint64(probabilityOfBeingSampled * float64(totalNumberOfSamples))
		variance                  = float64(totalNumberOfSamples) * probabilityOfBeingSampled * (1 - probabilityOfBeingSampled)
		stdDeviation              = math.Sqrt(variance)
		maxDeviation              uint64
	)
	for _, sampled := range proposerFrequency {
		maxDeviation = max(
			maxDeviation,
			safemath.AbsDiff(
				uint64(sampled),
				expectedNumberOfSamples,
			),
		)
	}

	maxSTDDeviation := float64(maxDeviation) / stdDeviation
	require.Less(maxSTDDeviation, 3.)
}

func makeValidators(t testing.TB, count int) ([]ids.NodeID, *validatorstest.State) {
	validatorIDs := make([]ids.NodeID, count)
	for i := range validatorIDs {
		validatorIDs[i] = ids.BuildTestNodeID([]byte{byte(i) + 1})
	}
	return validatorIDs, makeValidatorState(t, validatorIDs)
}

func makeValidatorState(t testing.TB, validatorIDs []ids.NodeID) *validatorstest.State {
	vdrState := &validatorstest.State{
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
	return vdrState
}
