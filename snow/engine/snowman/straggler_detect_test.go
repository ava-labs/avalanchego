// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestNodeWeights(t *testing.T) {
	nws := nodeWeights{
		{Weight: 100},
		{Weight: 50},
	}

	total, err := nws.totalWeight()
	require.NoError(t, err)
	require.Equal(t, uint64(150), total)
}

func TestNodeWeightsOverflow(t *testing.T) {
	nws := nodeWeights{
		{Weight: math.MaxUint64 - 100},
		{Weight: 110},
	}

	total, err := nws.totalWeight()
	require.ErrorIs(t, err, safemath.ErrOverflow)
	require.Equal(t, uint64(0), total)
}

func TestNodeWeights2Blocks(t *testing.T) {
	nw2b := nodeWeightsToHeight{
		ids.NodeWeight{Weight: 5}:  100,
		ids.NodeWeight{Weight: 10}: 101,
	}

	total, err := nw2b.totalWeight()
	require.NoError(t, err)
	require.Equal(t, uint64(15), total)
}

func TestGetNetworkSnapshot(t *testing.T) {
	n1 := ids.GenerateTestNodeID()

	n2 := ids.GenerateTestNodeID()

	connectedValidators := func(s []ids.NodeWeight) func() set.Set[ids.NodeWeight] {
		return func() set.Set[ids.NodeWeight] {
			var set set.Set[ids.NodeWeight]
			for _, nw := range s {
				set.Add(nw)
			}
			return set
		}
	}

	for _, testCase := range []struct {
		totalWeight           uint64
		description           string
		lastAcceptedHeight    uint64
		lastAcceptedFromNodes map[ids.NodeID]uint64
		connectedValidators   func() set.Set[ids.NodeWeight]
		expectedSnapshot      snapshot
		expectedOK            bool
		expectedLogged        string
	}{
		{
			description:         "connected to zero weight",
			connectedValidators: connectedValidators([]ids.NodeWeight{}),
			expectedLogged:      "connected to zero weight",
		},
		{
			description:         "did not gather enough statistics",
			totalWeight:         9999999999,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 1, ID: n1}, {Weight: 999999, ID: n2}}),
			lastAcceptedFromNodes: map[ids.NodeID]uint64{
				n1: 100,
			},
			expectedLogged: "Not collected enough information about last accepted block",
		},
		{
			description:         "we're in sync",
			totalWeight:         999999,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 999999, ID: n1}}),
			lastAcceptedFromNodes: map[ids.NodeID]uint64{
				n1: 100,
			},
			lastAcceptedHeight: 100,
			expectedOK:         true,
			expectedSnapshot: snapshot{totalValidatorWeight: 999999, lastAcceptedBlockHeight: nodeWeightsToHeight{
				{Weight: 999999, ID: n1}: 100,
			}},
		},
		{
			description:         "we're behind",
			totalWeight:         999999,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 999999, ID: n1}}),
			lastAcceptedFromNodes: map[ids.NodeID]uint64{
				n1: 120,
			},
			lastAcceptedHeight: 100,
			expectedSnapshot: snapshot{totalValidatorWeight: 999999, lastAcceptedBlockHeight: nodeWeightsToHeight{
				ids.NodeWeight{ID: n1, Weight: 999999}: 120,
			}},
			expectedOK: true,
		},
		{
			description:         "we're not behind",
			totalWeight:         999999,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 999999, ID: n1}}),
			lastAcceptedFromNodes: map[ids.NodeID]uint64{
				n1: 100,
			},
			lastAcceptedHeight: 100,
			expectedSnapshot: snapshot{totalValidatorWeight: 999999, lastAcceptedBlockHeight: nodeWeightsToHeight{
				ids.NodeWeight{ID: n1, Weight: 999999}: 100,
			}},
			expectedOK: true,
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			var buff logBuffer
			log := logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, &buff, logging.Plain.ConsoleEncoder()))

			s := &snapshotter{
				totalWeight: func() (uint64, error) {
					return testCase.totalWeight, nil
				},
				log:                      log,
				connectedValidators:      testCase.connectedValidators,
				minConfirmationThreshold: 0.75,
				lastAcceptedHeightByNodeID: func(vdr ids.NodeID) (ids.ID, uint64, bool) {
					height, ok := testCase.lastAcceptedFromNodes[vdr]
					return ids.Empty, height, ok
				},
			}

			snapshot, ok := s.getNetworkSnapshot()
			require.Equal(t, testCase.expectedSnapshot, snapshot)
			require.Equal(t, testCase.expectedOK, ok)
			require.Contains(t, buff.String(), testCase.expectedLogged)
		})
	}
}

func TestFailedCatchingUp(t *testing.T) {
	n1 := ids.GenerateTestNodeID()

	n2 := ids.GenerateTestNodeID()

	for _, testCase := range []struct {
		description           string
		lastAccepted          uint64
		lastAcceptedFromNodes map[ids.NodeID]ids.ID
		connectedValidators   []ids.NodeWeight
		input                 snapshot
		expected              bool
		expectedLogged        string
	}{
		{
			description: "stake overflow",
			input: snapshot{
				totalValidatorWeight: 100,
				lastAcceptedBlockHeight: nodeWeightsToHeight{
					ids.NodeWeight{ID: n1, Weight: math.MaxUint64 - 10}: 100,
					ids.NodeWeight{ID: n2, Weight: 11}:                  100,
				},
			},
			expectedLogged: "Failed computing total weight",
		},
		{
			description: "Straggling behind stake majority",
			input: snapshot{
				totalValidatorWeight: 100, lastAcceptedBlockHeight: nodeWeightsToHeight{
					ids.NodeWeight{ID: n1, Weight: 26}: 100,
					ids.NodeWeight{ID: n2, Weight: 50}: 100,
				},
			},
			expectedLogged: "We are straggling behind",
			expected:       true,
		},
		{
			description:  "In sync with the majority",
			lastAccepted: 100,
			input: snapshot{
				totalValidatorWeight: 100, lastAcceptedBlockHeight: nodeWeightsToHeight{
					ids.NodeWeight{ID: n1, Weight: 75}: 100,
					ids.NodeWeight{ID: n2, Weight: 25}: 100,
				},
			},
			expectedLogged: "Nodes ahead of us",
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			var buff logBuffer
			log := logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, &buff, logging.Plain.ConsoleEncoder()))

			sa := &snapshotAnalyzer{
				lastAcceptedHeight: func() uint64 {
					return testCase.lastAccepted
				},
				log: log,
			}

			require.Equal(t, testCase.expected, sa.areWeBehindTheRest(testCase.input))
			require.Contains(t, buff.String(), testCase.expectedLogged)
		})
	}
}

func TestCheckIfWeAreStragglingBehind(t *testing.T) {
	var fakeClock mockable.Clock

	snapshots := make(chan snapshot, 1)
	assertNoSnapshotsRemain := func() {
		select {
		case <-snapshots:
			require.Fail(t, "Should not have any snapshots in standby")
		default:
		}
	}
	nonEmptySnap := snapshot{
		totalValidatorWeight: 100,
		lastAcceptedBlockHeight: nodeWeightsToHeight{
			ids.NodeWeight{Weight: 100}: 100,
		},
	}

	var buff logBuffer
	log := logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, &buff, logging.Plain.ConsoleEncoder()))

	sd := stragglerDetector{
		stragglerDetectorConfig: stragglerDetectorConfig{
			minStragglerCheckInterval: time.Second,
			getTime:                   fakeClock.Time,
			log:                       log,
			getSnapshot: func() (snapshot, bool) {
				s := <-snapshots
				return s, !s.isEmpty()
			},
		},
	}

	fakeTime := time.Now()

	for _, testCase := range []struct {
		description                   string
		timeAdvanced                  time.Duration
		evalExtraAssertions           func(*testing.T, *stragglerDetector)
		expectedStragglingTime        time.Duration
		snapshotsRead                 []snapshot
		haveWeFailedCatchingUpReturns bool
	}{
		{
			description:         "First invocation only sets the time",
			evalExtraAssertions: func(_ *testing.T, _ *stragglerDetector) {},
		},
		{
			description:         "Should not check yet, as it is not time yet",
			timeAdvanced:        time.Millisecond * 500,
			evalExtraAssertions: func(_ *testing.T, _ *stragglerDetector) {},
		},
		{
			description:   "Advance time some more, so now we should check",
			timeAdvanced:  time.Millisecond * 501,
			snapshotsRead: []snapshot{{}},
			evalExtraAssertions: func(t *testing.T, _ *stragglerDetector) {
				require.Contains(t, buff.String(), "No node snapshot obtained")
			},
		},
		{
			description:                   "Advance time some more to the first check where the snapshot isn't empty",
			timeAdvanced:                  time.Second * 2,
			snapshotsRead:                 []snapshot{nonEmptySnap},
			haveWeFailedCatchingUpReturns: true,
			evalExtraAssertions: func(t *testing.T, _ *stragglerDetector) {
				require.Empty(t, buff.String())
			},
		},
		{
			description:                   "The next check returns we have failed catching up.",
			timeAdvanced:                  time.Second * 2,
			expectedStragglingTime:        time.Second * 2,
			haveWeFailedCatchingUpReturns: true,
			evalExtraAssertions: func(t *testing.T, sd *stragglerDetector) {
				require.Empty(t, sd.prevSnapshot)
			},
		},
		{
			description:                   "The third snapshot is due to a fresh check",
			timeAdvanced:                  time.Second * 2,
			snapshotsRead:                 []snapshot{nonEmptySnap},
			haveWeFailedCatchingUpReturns: true,
			// We carry over the total straggling time from previous testCase to this check,
			// as we expect the next check to nullify it.
			expectedStragglingTime: time.Second * 2,
			evalExtraAssertions:    func(_ *testing.T, _ *stragglerDetector) {},
		},
		{
			description:         "The fourth check returns we have succeeded in catching up",
			timeAdvanced:        time.Second * 2,
			evalExtraAssertions: func(_ *testing.T, _ *stragglerDetector) {},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			fakeTime = fakeTime.Add(testCase.timeAdvanced)
			fakeClock.Set(fakeTime)

			// Load the snapshot expected to be retrieved in this testCase, if applicable.
			if len(testCase.snapshotsRead) > 0 {
				snapshots <- testCase.snapshotsRead[0]
			}

			sd.areWeBehindTheRest = func(_ snapshot) bool {
				return testCase.haveWeFailedCatchingUpReturns
			}

			require.Equal(t, testCase.expectedStragglingTime, sd.CheckIfWeAreStragglingBehind())
			testCase.evalExtraAssertions(t, &sd)

			// Cleanup the log buffer, and make sure no snapshots remain for next testCase.
			buff.Reset()
			assertNoSnapshotsRemain()
		})
	}
}
