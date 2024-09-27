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
	nw2b := nodeWeightsToBlocks{
		ids.NodeWeight{Weight: 5}:  ids.Empty,
		ids.NodeWeight{Weight: 10}: ids.Empty,
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
		description           string
		lastAccepted          ids.ID
		lastAcceptedFromNodes map[ids.NodeID]ids.ID
		processing            map[ids.ID]struct{}
		connectedValidators   func() set.Set[ids.NodeWeight]
		connectedPercent      float64
		expectedSnapshot      snapshot
		expectedOK            bool
		expectedLogged        string
	}{
		{
			description:         "not enough connected validators",
			connectedValidators: connectedValidators([]ids.NodeWeight{}),
			expectedLogged:      "not enough connected stake to determine network info",
		},
		{
			description:         "connected to zero weight",
			connectedPercent:    1.0,
			connectedValidators: connectedValidators([]ids.NodeWeight{}),
			expectedLogged:      "Connected to zero weight",
		},
		{
			description:         "not enough info",
			connectedPercent:    1.0,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 1, Node: n1}, {Weight: 999999, Node: n2}}),
			lastAcceptedFromNodes: map[ids.NodeID]ids.ID{
				n1: {0x1},
			},
			expectedLogged: "Not collected enough information about last accepted blocks",
		},
		{
			description:         "we're in sync",
			connectedPercent:    1.0,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 999999, Node: n1}}),
			lastAcceptedFromNodes: map[ids.NodeID]ids.ID{
				n1: {0x1},
			},
			lastAccepted:   ids.ID{0x1},
			expectedLogged: "Most stake we're connected to has the same height as we do",
		},
		{
			description:         "we're behind",
			connectedPercent:    1.0,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 999999, Node: n1}}),
			lastAcceptedFromNodes: map[ids.NodeID]ids.ID{
				n1: {0x1},
			},
			processing:   map[ids.ID]struct{}{{0x1}: {}},
			lastAccepted: ids.ID{0x0},
			expectedSnapshot: snapshot{totalValidatorWeight: 999999, lastAccepted: nodeWeightsToBlocks{
				ids.NodeWeight{Node: n1, Weight: 999999}: {0x1},
			}},
			expectedOK: true,
		},
		{
			description:         "we're not behind",
			connectedPercent:    1.0,
			connectedValidators: connectedValidators([]ids.NodeWeight{{Weight: 999999, Node: n1}}),
			lastAcceptedFromNodes: map[ids.NodeID]ids.ID{
				n1: {0x1},
			},
			processing:   map[ids.ID]struct{}{{0x2}: {}},
			lastAccepted: ids.ID{0x0},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			var buff logBuffer
			log := logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, &buff, logging.Plain.ConsoleEncoder()))

			sd := newStragglerDetector(nil, log, 0.75,
				func() (ids.ID, uint64) {
					return testCase.lastAccepted, 0
				},
				testCase.connectedValidators, func() float64 { return testCase.connectedPercent },
				func(id ids.ID) bool {
					_, ok := testCase.processing[id]
					return ok
				},
				func(vdr ids.NodeID) (ids.ID, bool) {
					id, ok := testCase.lastAcceptedFromNodes[vdr]
					return id, ok
				})

			snapshot, ok, err := sd.getNetworkSnapshot()
			require.Equal(t, testCase.expectedSnapshot, snapshot)
			require.Equal(t, testCase.expectedOK, ok)
			require.NoError(t, err)
			require.Contains(t, buff.String(), testCase.expectedLogged)
		})
	}
}

func TestFailedCatchingUp(t *testing.T) {
	n1 := ids.GenerateTestNodeID()

	n2 := ids.GenerateTestNodeID()

	for _, testCase := range []struct {
		description           string
		lastAccepted          ids.ID
		lastAcceptedFromNodes map[ids.NodeID]ids.ID
		processing            map[ids.ID]struct{}
		connectedValidators   []ids.NodeWeight
		connectedPercent      float64
		input                 snapshot
		expected              bool
		expectedLogged        string
	}{
		{
			description: "stake overflow",
			input: snapshot{
				lastAccepted: nodeWeightsToBlocks{
					ids.NodeWeight{Node: n1, Weight: math.MaxUint64 - 10}: ids.ID{0x1},
					ids.NodeWeight{Node: n2, Weight: 11}:                  ids.ID{0x2},
				},
			},
			processing: map[ids.ID]struct{}{
				{0x1}: {},
				{0x2}: {},
			},
			expectedLogged: "Cumulative weight overflow",
		},
		{
			description: "Straggling behind stake minority",
			input: snapshot{
				totalValidatorWeight: 100, lastAccepted: nodeWeightsToBlocks{
					ids.NodeWeight{Node: n1, Weight: 25}: ids.ID{0x1},
					ids.NodeWeight{Node: n2, Weight: 50}: ids.ID{0x2},
				},
			},
			processing: map[ids.ID]struct{}{
				{0x1}: {},
				{0x2}: {},
			},
			expectedLogged: "Nodes ahead of us",
		},
		{
			description: "Straggling behind stake majority",
			input: snapshot{
				totalValidatorWeight: 100, lastAccepted: nodeWeightsToBlocks{
					ids.NodeWeight{Node: n1, Weight: 26}: ids.ID{0x1},
					ids.NodeWeight{Node: n2, Weight: 50}: ids.ID{0x2},
				},
			},
			processing: map[ids.ID]struct{}{
				{0x1}: {},
				{0x2}: {},
			},
			expectedLogged: "We are straggling behind",
			expected:       true,
		},
		{
			description: "In sync with the majority",
			input: snapshot{
				totalValidatorWeight: 100, lastAccepted: nodeWeightsToBlocks{
					ids.NodeWeight{Node: n1, Weight: 75}: ids.ID{0x1},
					ids.NodeWeight{Node: n2, Weight: 25}: ids.ID{0x2},
				},
			},
			processing: map[ids.ID]struct{}{
				{0x2}: {},
			},
			expectedLogged: "Nodes ahead of us",
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			var buff logBuffer
			log := logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, &buff, logging.Plain.ConsoleEncoder()))

			sd := newStragglerDetector(nil, log, 0.75,
				func() (ids.ID, uint64) {
					return testCase.lastAccepted, 0
				},
				func() set.Set[ids.NodeWeight] {
					var set set.Set[ids.NodeWeight]
					for _, nw := range testCase.connectedValidators {
						set.Add(nw)
					}
					return set
				}, func() float64 { return testCase.connectedPercent },
				func(id ids.ID) bool {
					_, ok := testCase.processing[id]
					return ok
				},
				func(vdr ids.NodeID) (ids.ID, bool) {
					id, ok := testCase.lastAcceptedFromNodes[vdr]
					return id, ok
				})

			require.Equal(t, testCase.expected, sd.failedCatchingUp(testCase.input))
			require.Contains(t, buff.String(), testCase.expectedLogged)
		})
	}
}

func TestCheckIfWeAreStragglingBehind(t *testing.T) {
	fakeClock := make(chan time.Time, 1)

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
		lastAccepted: nodeWeightsToBlocks{
			ids.NodeWeight{Weight: 100}: ids.Empty,
		},
	}

	var haveWeFailedCatchingUpReturns bool

	var buff logBuffer
	log := logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, &buff, logging.Plain.ConsoleEncoder()))

	sd := stragglerDetector{
		stragglerDetectorConfig: stragglerDetectorConfig{
			minStragglerCheckInterval: time.Second,
			getTime: func() time.Time {
				now := <-fakeClock
				return now
			},
			log: log,
			getSnapshot: func() (snapshot, bool, error) {
				s := <-snapshots
				return s, !s.isEmpty(), nil
			},
			haveWeFailedCatchingUp: func(_ snapshot) bool {
				return haveWeFailedCatchingUpReturns
			},
		},
	}

	fakeTime := time.Now()

	for _, testCase := range []struct {
		description                   string
		timeAdvanced                  time.Duration
		evalExtraAssertions           func(t *testing.T)
		expectedStragglingTime        time.Duration
		snapshotsRead                 []snapshot
		haveWeFailedCatchingUpReturns bool
	}{
		{
			description:         "First invocation only sets the time",
			evalExtraAssertions: func(_ *testing.T) {},
		},
		{
			description:         "Should not check yet, as it is not time yet",
			timeAdvanced:        time.Millisecond * 500,
			evalExtraAssertions: func(_ *testing.T) {},
		},
		{
			description:   "Advance time some more, so now we should check",
			timeAdvanced:  time.Millisecond * 501,
			snapshotsRead: []snapshot{{}},
			evalExtraAssertions: func(t *testing.T) {
				require.Contains(t, buff.String(), "No node snapshot obtained")
			},
		},
		{
			description:   "Advance time some more to the first check where the snapshot isn't empty",
			timeAdvanced:  time.Second * 2,
			snapshotsRead: []snapshot{nonEmptySnap},
			evalExtraAssertions: func(t *testing.T) {
				require.Empty(t, buff.String())
			},
		},
		{
			description:                   "The next check returns we have failed catching up.",
			timeAdvanced:                  time.Second * 2,
			expectedStragglingTime:        time.Second * 2,
			haveWeFailedCatchingUpReturns: true,
			evalExtraAssertions: func(t *testing.T) {
				require.Empty(t, sd.prevSnapshot)
			},
		},
		{
			description:   "The third snapshot is due to a fresh check",
			timeAdvanced:  time.Second * 2,
			snapshotsRead: []snapshot{nonEmptySnap},
			// We carry over the total straggling time from previous testCase to this check,
			// as we need the next check to nullify it.
			expectedStragglingTime: time.Second * 2,
			evalExtraAssertions:    func(_ *testing.T) {},
		},
		{
			description:         "The fourth check returns we have succeeded in catching up",
			timeAdvanced:        time.Second * 2,
			evalExtraAssertions: func(_ *testing.T) {},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			fakeTime = fakeTime.Add(testCase.timeAdvanced)
			fakeClock <- fakeTime

			// Load the snapshot expected to be retrieved in this testCase, if applicable.
			if len(testCase.snapshotsRead) > 0 {
				snapshots <- testCase.snapshotsRead[0]
			}

			haveWeFailedCatchingUpReturns = testCase.haveWeFailedCatchingUpReturns
			require.Equal(t, testCase.expectedStragglingTime, sd.CheckIfWeAreStragglingBehind())
			testCase.evalExtraAssertions(t)

			// Cleanup the log buffer, and make sure no snapshots remain for next testCase.
			buff.Reset()
			assertNoSnapshotsRemain()
			haveWeFailedCatchingUpReturns = false
		})
	}
}
