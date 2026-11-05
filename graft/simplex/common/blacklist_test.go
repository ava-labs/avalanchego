// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"crypto/rand"
	"fmt"
	"math"
	mathrand "math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlacklistVerifyProposedBlacklist(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		blacklist         Blacklist
		proposedBlacklist Blacklist
		nodeCount         uint16
		round             uint64
		expectedErr       error
	}{
		{
			name: "max blacklisted rounds increased by more than one orbit",
			blacklist: Blacklist{
				maxBlacklistedRounds: 10,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 15,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedRounds,
		},
		{
			name: "max blacklisted rounds decreased by more than one orbit",
			blacklist: Blacklist{
				maxBlacklistedRounds: 10,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 5,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedRounds,
		},
		{
			name: "max blacklisted rounds decreased below node count",
			blacklist: Blacklist{
				maxBlacklistedRounds: 5,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 3,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedRounds,
		},
		{
			name: "max blacklisted rounds decreased to zero",
			blacklist: Blacklist{
				maxBlacklistedRounds: 4,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 0,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedRounds,
		},
		{
			name: "max blacklisted rounds overflows",
			blacklist: Blacklist{
				maxBlacklistedRounds: math.MaxUint16,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 0,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedRounds,
		},
		{
			name: "invalid blacklist update",
			blacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 0}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 0}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 3}},
			},
			expectedErr: errBlacklistInvalidUpdates,
		},
		{
			name: "node redeemed in the same orbit it was suspected in",
			blacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 7, RedeemingWeight: 0, OrbitToRedeem: 0}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 7, RedeemingWeight: 1, OrbitToRedeem: 7}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3}},
			},
			expectedErr: errBlacklistInvalidBlacklist,
		},
		{
			name: "blacklist mismatch",
			blacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 0}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 7}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3}},
			},
			expectedErr: errBlacklistInvalidBlacklist,
		},
		{
			name: "blacklist mismatch - too many blacklisted",
			blacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}, {NodeIndex: 2, SuspectingWeight: 2}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}, {NodeIndex: 2, SuspectingWeight: 2}, {NodeIndex: 1, SuspectingWeight: 1}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 1}},
			},
			expectedErr: errBlacklistInvalidBlacklist,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.blacklist.VerifyProposedBlacklist(nodesWithWeights(equalWeights(testCase.nodeCount)), testCase.proposedBlacklist, testCase.round)
			require.ErrorContains(t, err, testCase.expectedErr.Error())
		})
	}
}

func TestOrbit(t *testing.T) {
	nodes := []uint16{0, 1, 2, 3}

	for _, testCase := range []struct {
		round               uint64
		expectedNodeToOrbit map[uint16]uint64
	}{
		{
			round: 0,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 1,
				1: 0,
				2: 0,
				3: 0,
			},
		},
		{
			round: 1,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 1,
				1: 1,
				2: 0,
				3: 0,
			},
		},
		{
			round: 2,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 1,
				1: 1,
				2: 1,
				3: 0,
			},
		},
		{
			round: 3,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 1,
				1: 1,
				2: 1,
				3: 1,
			},
		},
		{
			round: 4,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 2,
				1: 1,
				2: 1,
				3: 1,
			},
		},
		{
			round: 5,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 2,
				1: 2,
				2: 1,
				3: 1,
			},
		},
		{
			round: 6,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 2,
				1: 2,
				2: 2,
				3: 1,
			},
		},
		{
			round: 7,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 2,
				1: 2,
				2: 2,
				3: 2,
			},
		},
		{
			round: 8,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 3,
				1: 2,
				2: 2,
				3: 2,
			},
		},
		{
			round: 9,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 3,
				1: 3,
				2: 2,
				3: 2,
			},
		},
		{
			round: 10,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 3,
				1: 3,
				2: 3,
				3: 2,
			},
		},
		{
			round: 11,
			expectedNodeToOrbit: map[uint16]uint64{
				0: 3,
				1: 3,
				2: 3,
				3: 3,
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", testCase.round), func(t *testing.T) {
			for node, expectedOrbit := range testCase.expectedNodeToOrbit {
				require.Equal(t, expectedOrbit, orbit(testCase.round, node, uint16(len(nodes))), "unexpected OrbitSuspected for node %d", node)
			}
		})
	}
}

func TestBlacklistFromBytes(t *testing.T) {
	populated := Blacklist{
		maxBlacklistedRounds: 7,
		suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 1, OrbitSuspected: 1}},
		updates:              BlacklistUpdates{{NodeIndex: 3, Type: BlacklistOpType_NodeSuspected}},
	}

	for _, testCase := range []struct {
		name              string
		data              []byte
		expectErr         bool
		expectedBlacklist Blacklist
	}{
		{
			name:              "empty blacklist",
			data:              (&Blacklist{maxBlacklistedRounds: 100}).Bytes(),
			expectedBlacklist: Blacklist{maxBlacklistedRounds: 100},
		},
		{
			name:              "populated blacklist",
			data:              populated.Bytes(),
			expectedBlacklist: populated,
		},
		{
			name:      "malformed data",
			data:      []byte{0, 1, 2},
			expectErr: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var bl Blacklist
			err := bl.FromBytes(testCase.data)
			if testCase.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, testCase.expectedBlacklist.Equals(&bl))
		})
	}
}

func TestBlacklistSimpleFlow(t *testing.T) {
	const nodeCount = 4
	weights := equalWeights(nodeCount)

	var bl Blacklist
	require.False(t, bl.IsNodeSuspected(2, nodeCount))

	bl.setNodeSuspected(1, 2, 1, weights, nodeCount)
	require.False(t, bl.IsNodeSuspected(2, nodeCount))

	bl.setNodeSuspected(1, 2, 1, weights, nodeCount)
	require.True(t, bl.IsNodeSuspected(2, nodeCount))

	bl.suspectedNodes = bl.garbageCollectSuspectedNodes(nodeCount, 5, nodeCount)
	require.True(t, bl.IsNodeSuspected(2, nodeCount), "Suspected node is still suspected even if its OrbitSuspected advanced")

	bl.setNodeRedeemed(3, 2, 1, nodeCount)
	require.True(t, bl.IsNodeSuspected(2, nodeCount))

	bl.setNodeRedeemed(3, 2, 1, nodeCount)
	require.False(t, bl.IsNodeSuspected(2, nodeCount))
}

// TestBlacklistCapAppliesToPendingSuspects asserts that at most f nodes can be blacklisted.
func TestBlacklistCapAppliesToPendingSuspects(t *testing.T) {
	const nodeCount = 4
	f := (nodeCount - 1) / 3
	require.Equal(t, 1, f)
	weights := equalWeights(nodeCount)

	countBlacklisted := func(bl Blacklist) int {
		blacklisted := 0
		for node := uint16(0); node < nodeCount; node++ {
			if bl.IsNodeSuspected(node, nodeCount) {
				blacklisted++
			}
		}
		return blacklisted
	}

	t.Run("setNodeSuspected", func(t *testing.T) {
		var bl Blacklist

		// One accusation each: both nodes are pending, neither is blacklisted.
		bl.setNodeSuspected(1, 2, 1, weights, nodeCount)
		bl.setNodeSuspected(1, 3, 1, weights, nodeCount)
		require.Len(t, bl.suspectedNodes, 2)
		require.Zero(t, countBlacklisted(bl))

		// A second accusation blacklists node 2 and fills the cap.
		bl.setNodeSuspected(1, 2, 1, weights, nodeCount)
		require.True(t, bl.IsNodeSuspected(2, nodeCount))

		// A second accusation of the pending node 3 must not blacklist it: f nodes are already blacklisted.
		bl.setNodeSuspected(1, 3, 1, weights, nodeCount)
		require.False(t, bl.IsNodeSuspected(3, nodeCount))
		require.LessOrEqual(t, countBlacklisted(bl), f, "%s", bl.String())
	})

	t.Run("Next", func(t *testing.T) {
		// Two consecutive rounds within one orbit for both accused nodes, as two successive leaders
		// would produce when accusing the same pair.
		const firstRound, secondRound = 2, 3
		for _, node := range []uint16{0, 1} {
			require.Equal(t, orbit(firstRound, node, nodeCount), orbit(secondRound, node, nodeCount))
		}

		var bl Blacklist
		bl = bl.Next(nodesWithWeights(weights), firstRound, 0, map[uint16]uint64{0: firstRound, 1: firstRound}, nil)
		require.Len(t, bl.suspectedNodes, 2)
		require.Zero(t, countBlacklisted(bl))

		bl = bl.Next(nodesWithWeights(weights), secondRound, 0, map[uint16]uint64{0: secondRound, 1: secondRound}, nil)
		require.LessOrEqual(t, countBlacklisted(bl), f, "%s", bl.String())
	})
}

func FuzzBlacklistFromBytes(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		var blacklist Blacklist
		err := blacklist.FromBytes(data)
		if err == nil {
			data2 := blacklist.Bytes()
			require.Equal(t, data, data2)
			return
		}
		require.Error(t, err)
	})
}

func TestBlacklistBytes(t *testing.T) {
	bl := Blacklist{
		maxBlacklistedRounds: 5,
		suspectedNodes: []SuspectedNode{
			{NodeIndex: 1, SuspectingWeight: 2, OrbitSuspected: 3, RedeemingWeight: 4, OrbitToRedeem: 5},
			{NodeIndex: 6, SuspectingWeight: 7, OrbitSuspected: 8, RedeemingWeight: 9, OrbitToRedeem: 10},
		},
		updates: []BlacklistUpdate{
			{Type: BlacklistOpType_NodeSuspected, NodeIndex: 11},
			{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 12},
		},
	}
	bytes := bl.Bytes()

	var bl2 Blacklist
	err := bl2.FromBytes(bytes)
	require.NoError(t, err)

	require.True(t, bl.Equals(&bl2))
}

func TestComputeBlacklistUpdates(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		round              uint64
		suspectedNodes     []SuspectedNode
		expectedUpdates    BlacklistUpdates
		timedOut, redeemed map[uint16]uint64
	}{
		{
			name:            "empty suspected nodes, a node has timed out",
			expectedUpdates: []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			timedOut:        map[uint16]uint64{2: 10},
			round:           13,
		},
		{
			name:            "empty suspected nodes, a node has timed out and later has been redeemed",
			expectedUpdates: make([]BlacklistUpdate, 0, 4),
			timedOut:        map[uint16]uint64{2: 10},
			redeemed:        map[uint16]uint64{2: 11},
			round:           13,
		},
		{
			name:            "a node is suspected and has timed out again",
			suspectedNodes:  []SuspectedNode{{NodeIndex: 2, SuspectingWeight: 2}},
			expectedUpdates: make([]BlacklistUpdate, 0, 4),
			timedOut:        map[uint16]uint64{2: 10},
			round:           13,
		},
		{
			name:            "a node is suspected and has been redeemed",
			suspectedNodes:  []SuspectedNode{{NodeIndex: 2, SuspectingWeight: 2}},
			expectedUpdates: []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 2}},
			timedOut:        map[uint16]uint64{2: 10},
			redeemed:        map[uint16]uint64{2: 11},
			round:           13,
		},
		{
			name:            "a node is not suspected and is now suspected",
			suspectedNodes:  []SuspectedNode{{NodeIndex: 2, SuspectingWeight: 1}},
			expectedUpdates: []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			timedOut:        map[uint16]uint64{2: 10},
			redeemed:        map[uint16]uint64{2: 9},
			round:           13,
		},
		{
			name:            "a node is not suspected and is suspected and then redeemed",
			expectedUpdates: make([]BlacklistUpdate, 0, 4),
			suspectedNodes:  []SuspectedNode{{NodeIndex: 2, SuspectingWeight: 1}},
			timedOut:        map[uint16]uint64{2: 10},
			redeemed:        map[uint16]uint64{2: 11},
			round:           13,
		},
		{
			name:            "a node is not suspected and also hasn't timed out",
			suspectedNodes:  []SuspectedNode{{NodeIndex: 2, SuspectingWeight: 1}},
			expectedUpdates: make([]BlacklistUpdate, 0, 4),
			timedOut:        map[uint16]uint64{2: 9},
		},
		{
			name:            "a node is suspected and hasn't been redeemed",
			suspectedNodes:  []SuspectedNode{{NodeIndex: 2, SuspectingWeight: 2}},
			expectedUpdates: make([]BlacklistUpdate, 0, 4),
			timedOut:        map[uint16]uint64{2: 11},
			redeemed:        map[uint16]uint64{2: 10},
			round:           13,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			blacklist := Blacklist{
				suspectedNodes: testCase.suspectedNodes,
			}

			next := blacklist.Next(nodesWithWeights(equalWeights(4)), testCase.round, 0, testCase.timedOut, testCase.redeemed)
			require.Equal(t, testCase.expectedUpdates, next.updates)
		})
	}
}

func TestAdvanceRound(t *testing.T) {
	nodes := []uint16{0, 1, 2, 3}

	// NodeIDs 0, 2 are suspected.
	// NodeIDs 1 and 3 are not suspected.
	// Node 2 can be redeemed.
	suspectedNodesBefore := suspectedNodes{
		{NodeIndex: 0, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 1, OrbitToRedeem: 1},
		{NodeIndex: 1, SuspectingWeight: 1, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
		{NodeIndex: 2, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 2, OrbitToRedeem: 1},
		{NodeIndex: 3, SuspectingWeight: 1, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
	}

	testCases := []struct {
		suspectedNodesAfter suspectedNodes
		noChange            bool
	}{
		{
			// round 0
			noChange: true,
		},
		{
			// round 1
			noChange: true,
		},
		{
			// round 2
			noChange: true,
		},
		{
			// round 3
			noChange: true,
		},
		{
			// round 4
			suspectedNodesAfter: suspectedNodes{
				{NodeIndex: 0, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
				{NodeIndex: 1, SuspectingWeight: 1, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
				{NodeIndex: 2, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 2, OrbitToRedeem: 1},
				{NodeIndex: 3, SuspectingWeight: 1, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
			},
		},
		{
			// round 5
			suspectedNodesAfter: suspectedNodes{
				{NodeIndex: 0, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
				{NodeIndex: 2, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 2, OrbitToRedeem: 1},
				{NodeIndex: 3, SuspectingWeight: 1, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
			},
		},
		{
			// round 6
			suspectedNodesAfter: suspectedNodes{
				{NodeIndex: 0, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
				{NodeIndex: 3, SuspectingWeight: 1, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
			},
		},
		{
			// round 7
			suspectedNodesAfter: suspectedNodes{
				{NodeIndex: 0, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
			},
		},
		{
			// round 8
			suspectedNodesAfter: suspectedNodes{
				{NodeIndex: 0, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
			},
		},
		{
			// round 9
			suspectedNodesAfter: suspectedNodes{
				{NodeIndex: 0, SuspectingWeight: 2, OrbitSuspected: 1, RedeemingWeight: 0, OrbitToRedeem: 0},
			},
		},
	}

	for round := uint64(0); round < uint64(len(testCases)); round++ {
		t.Run(fmt.Sprintf("round %d", round), func(t *testing.T) {
			nodeCount := uint16(len(nodes))
			blacklist := Blacklist{
				maxBlacklistedRounds: 10 * nodeCount,
				suspectedNodes:       suspectedNodesBefore,
			}

			suspectingNodes := blacklist.garbageCollectSuspectedNodes(nodeCount, round, uint64(nodeCount))

			if testCases[round].noChange {
				require.Equal(t, suspectedNodesBefore, suspectingNodes)
			} else {
				require.Equal(t, testCases[round].suspectedNodesAfter, suspectingNodes)
			}
		})
	}
}

func TestVerifyBlacklistUpdates(t *testing.T) {
	const nodeCount = 4

	for _, testCase := range []struct {
		name        string
		Blacklist   Blacklist
		updates     []BlacklistUpdate
		expectedErr error
	}{
		{
			name: "too many updates",
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 1},
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 2},
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3},
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 4},
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 5},
			},
			expectedErr: errBlacklistTooManyUpdates,
		},
		{
			name: "invalid type",
			updates: []BlacklistUpdate{
				{Type: 3, NodeIndex: 1},
			},
			expectedErr: errBlacklistInvalidUpdateType,
		},
		{
			name: "invalid index",
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 4},
			},
			expectedErr: errBlacklistInvalidNodeIndex,
		},
		{
			name: "double vote",
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 3},
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2},
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 3},
			},
			expectedErr: errBlacklistNodeIndexAlreadyUpdated,
		},
		{
			name: "already blacklisted",
			Blacklist: Blacklist{
				suspectedNodes: []SuspectedNode{{NodeIndex: 3, SuspectingWeight: 2}},
			},
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 3},
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2},
			},
			expectedErr: errBlacklistNodeAlreadyBlacklisted,
		},
		{
			name: "not blacklisted, cannot be redeemed",
			Blacklist: Blacklist{
				suspectedNodes: []SuspectedNode{{NodeIndex: 3, SuspectingWeight: 1}},
			},
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3},
			},
			expectedErr: errBlacklistNodeNotBlacklisted,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.Blacklist.verifyBlacklistUpdates(nodeCount, testCase.updates, nodeCount)
			require.ErrorContains(t, err, testCase.expectedErr.Error())
		})
	}
}

func TestBlacklistMultipleAttemptsOverSeveralOrbits(t *testing.T) {
	// This test tests that even though a node is suspected continuously across several rounds,
	// as long as the rounds are too far from each other, the node will not be blacklisted.
	const nodeCount = 4
	weights := equalWeights(nodeCount)

	var blacklist Blacklist

	blacklist = blacklist.Next(nodesWithWeights(weights), 4, 0, map[uint16]uint64{3: 4}, nil)
	require.False(t, blacklist.IsNodeSuspected(3, nodeCount))

	blacklist = blacklist.Next(nodesWithWeights(weights), 7, 0, map[uint16]uint64{3: 7}, nil)
	require.False(t, blacklist.IsNodeSuspected(3, nodeCount))

	blacklist = blacklist.Next(nodesWithWeights(weights), 8, 0, map[uint16]uint64{3: 8}, nil)
	require.True(t, blacklist.IsNodeSuspected(3, nodeCount))
}

func TestBlacklistSimulateNetwork(t *testing.T) {
	for nodes := 1; nodes <= 128; nodes *= 2 {
		for rounds := 10; rounds <= 1000; rounds *= 10 {
			t.Run(fmt.Sprintf("n=%d/r=%d", nodes, rounds), func(t *testing.T) {
				t.Parallel()
				testBlacklistSimulateNetwork(t, equalWeights(uint16(nodes)), rounds)
			})
		}
	}
}

func testBlacklistSimulateNetwork(t *testing.T, weights []uint64, rounds int) {
	// This test simulates a network of len(weights) nodes with the given weights for 'rounds' rounds.
	// It ensures that blacklists that are created by a leader node,
	// can be verified by other nodes.

	nodeCount := len(weights)

	var blacklist Blacklist
	blacklist.maxBlacklistedRounds = uint16(nodeCount)

	crashedNodes := make(map[uint16]struct{})

	type (
		coinTossResult bool
	)

	const (
		tails coinTossResult = false
		heads coinTossResult = true
	)

	coinToss := func() coinTossResult {
		var b [1]byte
		_, err := rand.Read(b[:])
		require.NoError(t, err)
		return b[0]&1 == 0
	}

	leaderCrashedInRound := make(map[uint16]map[uint64]struct{})
	leaderRecoveredInRound := make(map[uint16]map[uint64]struct{})

	for node := uint16(0); node < uint16(nodeCount); node++ {
		leaderCrashedInRound[node] = make(map[uint64]struct{})
		leaderRecoveredInRound[node] = make(map[uint64]struct{})
	}

	for round := uint64(0); round < uint64(rounds); round++ {
		f := (nodeCount - 1) / 3
		// Try to crash up to `f` nodes.
		for len(crashedNodes) < f {
			if coinToss() == heads {
				nodeToCrash := uint16(mathrand.Intn(nodeCount))
				if _, crashed := crashedNodes[nodeToCrash]; !crashed {
					crashedNodes[nodeToCrash] = struct{}{}
					leaderCrashedInRound[nodeToCrash][round] = struct{}{}
				}
			}
		}

		// Try to recover each crashed node with probability of 1/2
		for node := range crashedNodes {
			if coinToss() == heads {
				delete(crashedNodes, node)
				leaderRecoveredInRound[node][round] = struct{}{}
				delete(crashedNodes, node)
				delete(leaderCrashedInRound[node], round)
			}
		}

		blacklist = simulateRound(t, blacklistRoundSimulationInput{
			round:            round,
			crashedInRound:   leaderCrashedInRound,
			recoveredInRound: leaderRecoveredInRound,
			crashedNodes:     crashedNodes,
			nodeCount:        nodeCount,
			weights:          weights,
			prevBlacklist:    blacklist,
		})
	}
}

type blacklistRoundSimulationInput struct {
	round            uint64
	crashedInRound   map[uint16]map[uint64]struct{}
	recoveredInRound map[uint16]map[uint64]struct{}
	crashedNodes     map[uint16]struct{}
	nodeCount        int
	weights          []uint64
	prevBlacklist    Blacklist
}

func simulateRound(t *testing.T, blrsi blacklistRoundSimulationInput) Blacklist {
	round := blrsi.round
	crashedInRound := blrsi.crashedInRound
	recoveredInRound := blrsi.recoveredInRound
	crashedNodes := blrsi.crashedNodes
	nodeCount := blrsi.nodeCount
	weights := blrsi.weights
	prevBlacklist := blrsi.prevBlacklist
	totalWeight := uint64s(weights).Sum()

	leader := uint16(round % uint64(nodeCount))
	if _, leaderCrashed := crashedNodes[leader]; leaderCrashed {
		// leader is crashed, nothing further that can be done.
		newBlacklist := Blacklist{
			maxBlacklistedRounds: prevBlacklist.maxBlacklistedRounds,
			suspectedNodes:       make([]SuspectedNode, len(prevBlacklist.suspectedNodes)),
			updates:              make([]BlacklistUpdate, 0, len(prevBlacklist.updates)),
		}
		copy(newBlacklist.suspectedNodes, prevBlacklist.suspectedNodes)
		return newBlacklist
	}

	// find the last round in which the node has crashed or recovered
	timedOut := make(map[uint16]uint64)
	redeemed := make(map[uint16]uint64)

	for r := round; r > 0; r-- {
		for node := uint16(0); node < uint16(nodeCount); node++ {
			if _, crashed := crashedInRound[node][r]; crashed && len(timedOut) == 0 {
				timedOut[node] = r
			}
			if _, recovered := recoveredInRound[node][r]; recovered && len(redeemed) == 0 {
				redeemed[node] = r
			}
		}
	}

	// The blacklist starts with a single orbit's worth of max blacklisted rounds, which is the floor it stays at for a target of zero.
	newBlacklist := prevBlacklist.Next(nodesWithWeights(weights), round, 0, timedOut, redeemed)

	err := prevBlacklist.VerifyProposedBlacklist(nodesWithWeights(weights), newBlacklist, round)
	require.NoError(t, err, "round %d", round)

	// The blacklisted nodes must never hold a threshold of the total weight.
	var blacklistedWeight uint64
	for node := uint16(0); node < uint16(nodeCount); node++ {
		if newBlacklist.IsNodeSuspected(node, totalWeight) {
			blacklistedWeight += weights[node]
		}
	}
	require.Less(t, blacklistedWeight, newBlacklist.computeThreshold(totalWeight), "round %d: %s", round, newBlacklist.String())

	return newBlacklist
}

func TestBlacklistEquals(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		bl1, bl2 Blacklist
		equals   bool
	}{
		{
			name: "different max blacklisted rounds",
			bl1: Blacklist{
				maxBlacklistedRounds: 1,
			},
			bl2: Blacklist{
				maxBlacklistedRounds: 2,
			},
		},
		{
			name: "different suspected nodes count",
			bl1: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
			},
			bl2: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}, {NodeIndex: 2, SuspectingWeight: 1}},
			},
		},
		{
			name: "different suspected nodes content",
			bl1: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
			},
			bl2: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 1}},
			},
		},
		{
			name: "different updates count",
			bl1: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			bl2: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}, {Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3}},
			},
		},
		{
			name: "different updates content",
			bl1: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			bl2: Blacklist{
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 2}},
			},
		},
		{
			name: "equal blacklists",
			bl1: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			bl2: Blacklist{
				maxBlacklistedRounds: 4,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			equals: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.equals, testCase.bl1.Equals(&testCase.bl2))
		})
	}
}

func TestBlacklistMaxBlacklistedOrbits(t *testing.T) {
	for _, testCase := range []struct {
		name                 string
		nodeCount            uint16
		maxBlacklistedRounds uint16
		expectedOrbits       uint64
	}{
		{
			name:                 "rounds are converted to whole orbits",
			nodeCount:            4,
			maxBlacklistedRounds: 4620,
			expectedOrbits:       1155,
		},
		{
			name:                 "rounds not divisible by node count are rounded down",
			nodeCount:            7,
			maxBlacklistedRounds: 20,
			expectedOrbits:       2,
		},
		{
			name:                 "zero node count does not divide by zero",
			nodeCount:            0,
			maxBlacklistedRounds: 9,
			expectedOrbits:       9,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bl := NewBlacklist(testCase.maxBlacklistedRounds)
			require.Equal(t, testCase.maxBlacklistedRounds, bl.maxBlacklistedRounds)
			require.Equal(t, testCase.expectedOrbits, bl.maxBlacklistedOrbits(testCase.nodeCount))
		})
	}
}

func TestBlacklistMaxBlacklistedRoundsIsPreserved(t *testing.T) {
	bl := NewBlacklist(4620)
	require.Equal(t, uint16(4620), bl.maxBlacklistedRounds)

	applied := bl.Next(nodesWithWeights(equalWeights(4)), 5, 4620, map[uint16]uint64{1: 5}, nil)
	require.Equal(t, bl.maxBlacklistedRounds, applied.maxBlacklistedRounds, "Next should carry over maxBlacklistedRounds when it is already at the target")

	cloned := applied.Clone()
	require.Equal(t, bl.maxBlacklistedRounds, cloned.maxBlacklistedRounds, "Clone should carry over maxBlacklistedRounds")
	require.True(t, applied.Equals(&cloned))

	var decoded Blacklist
	require.NoError(t, decoded.FromBytes(applied.Bytes()))
	require.Equal(t, bl.maxBlacklistedRounds, decoded.maxBlacklistedRounds, "serialization should preserve maxBlacklistedRounds")
	require.True(t, applied.Equals(&decoded))
}

func TestBlacklistedNodeExpiresAfterMaxBlacklistedOrbits(t *testing.T) {
	const (
		nodeCount            = 4
		maxBlacklistedOrbits = 2
		maxBlacklistedRounds = nodeCount * maxBlacklistedOrbits
		blacklistedNode      = 1
		orbitSuspected       = 3
	)

	// With 4 nodes, node 1 is in orbit k during rounds 4(k-1)+1 ... 4k.
	// Node 1 is blacklisted in orbit 3, and is allowed to remain blacklisted for 2 orbits (3 and 4).
	// It should be removed from the blacklist once orbit 5 is reached.
	blacklisted := SuspectedNode{NodeIndex: blacklistedNode, SuspectingWeight: 2, OrbitSuspected: orbitSuspected}

	for _, testCase := range []struct {
		name          string
		round         uint64
		suspectedNode SuspectedNode
		expectExpired bool
	}{
		{
			name:          "same orbit it was suspected in",
			round:         12, // orbit 3
			suspectedNode: blacklisted,
		},
		{
			name:          "last orbit it is allowed to remain blacklisted in",
			round:         16, // orbit 4
			suspectedNode: blacklisted,
		},
		{
			name:          "first round of the orbit after the last allowed orbit",
			round:         17, // orbit 5
			suspectedNode: blacklisted,
			expectExpired: true,
		},
		{
			name:          "long after the last allowed orbit",
			round:         1000,
			suspectedNode: blacklisted,
			expectExpired: true,
		},
		{
			name:          "partial redeeming votes do not prevent expiry",
			round:         17, // orbit 5
			suspectedNode: SuspectedNode{NodeIndex: blacklistedNode, SuspectingWeight: 2, OrbitSuspected: orbitSuspected, RedeemingWeight: 1, OrbitToRedeem: 4},
			expectExpired: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bl := Blacklist{
				maxBlacklistedRounds: maxBlacklistedRounds,
				suspectedNodes:       suspectedNodes{testCase.suspectedNode},
			}
			require.True(t, bl.IsNodeSuspected(blacklistedNode, nodeCount))

			next := bl.Next(nodesWithWeights(equalWeights(nodeCount)), testCase.round, maxBlacklistedRounds, nil, nil)
			require.Equal(t, uint16(maxBlacklistedRounds), next.maxBlacklistedRounds)

			if testCase.expectExpired {
				require.False(t, next.IsNodeSuspected(blacklistedNode, nodeCount))
				require.Empty(t, next.suspectedNodes)
			} else {
				require.True(t, next.IsNodeSuspected(blacklistedNode, nodeCount))
				require.Len(t, next.suspectedNodes, 1)
			}

			// The blacklist of the previous round must not be modified.
			require.True(t, bl.IsNodeSuspected(blacklistedNode, nodeCount))
		})
	}
}

func TestBlacklistedNodeExpiryEndToEnd(t *testing.T) {
	const (
		nodeCount            = 4
		maxBlacklistedRounds = 8 // 2 orbits
		blacklistedNode      = 1
	)
	weights := equalWeights(nodeCount)

	bl := NewBlacklist(maxBlacklistedRounds)
	require.Equal(t, uint64(2), bl.maxBlacklistedOrbits(nodeCount))

	// suspect makes the leader of the given round accuse the blacklisted node of timing out in that round.
	suspect := func(round uint64) map[uint16]uint64 {
		return map[uint16]uint64{blacklistedNode: round}
	}

	// Round 2 and 3 are in orbit 1 for node 1. Two accusations blacklist it (f+1 = 2).
	bl = bl.Next(nodesWithWeights(weights), 2, maxBlacklistedRounds, suspect(2), nil)
	require.False(t, bl.IsNodeSuspected(blacklistedNode, nodeCount))
	bl = bl.Next(nodesWithWeights(weights), 3, maxBlacklistedRounds, suspect(3), nil)
	require.True(t, bl.IsNodeSuspected(blacklistedNode, nodeCount))

	// Round 4 is still orbit 1 for node 1, and orbit 2 spans rounds 5-8: still blacklisted.
	for round := uint64(4); round <= 8; round++ {
		bl = bl.Next(nodesWithWeights(weights), round, maxBlacklistedRounds, nil, nil)
		require.True(t, bl.IsNodeSuspected(blacklistedNode, nodeCount), "round %d", round)
	}

	// orbit 3 (round 9): expired.
	bl = bl.Next(nodesWithWeights(weights), 9, maxBlacklistedRounds, nil, nil)
	require.False(t, bl.IsNodeSuspected(blacklistedNode, nodeCount))
	require.Empty(t, bl.suspectedNodes)

	// A redeem update for an expired node is rejected, as the node is no longer blacklisted.
	redeem := []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: blacklistedNode}}
	require.ErrorContains(t, bl.verifyBlacklistUpdates(nodeCount, redeem, nodeCount), errBlacklistNodeNotBlacklisted.Error())

	// An expired node can be suspected again from scratch.
	bl = bl.Next(nodesWithWeights(weights), 10, maxBlacklistedRounds, suspect(10), nil)
	require.False(t, bl.IsNodeSuspected(blacklistedNode, nodeCount))
	require.Equal(t, suspectedNodes{{NodeIndex: blacklistedNode, SuspectingWeight: 1, OrbitSuspected: 3}}, bl.suspectedNodes)
}

func TestBlacklistVerifyProposedBlacklistMaxBlacklistedRoundsStep(t *testing.T) {
	const nodeCount = 4
	weights := equalWeights(nodeCount)

	for _, testCase := range []struct {
		name     string
		prev     uint16
		proposed uint16
	}{
		{name: "unchanged", prev: 10, proposed: 10},
		{name: "increased by less than an orbit", prev: 10, proposed: 12},
		{name: "increased by one orbit", prev: 10, proposed: 14},
		{name: "decreased by less than an orbit", prev: 10, proposed: 8},
		{name: "decreased by one orbit", prev: 10, proposed: 6},
		{name: "decreased to the node count", prev: 6, proposed: 4},
		{name: "increased to the maximum", prev: math.MaxUint16 - nodeCount, proposed: math.MaxUint16},
		{name: "increased from zero", prev: 0, proposed: 4},
		{name: "increased from below the node count", prev: 1, proposed: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			prev := Blacklist{maxBlacklistedRounds: testCase.prev}
			proposed := prev.Next(nodesWithWeights(weights), 5, 0, nil, nil)
			proposed.maxBlacklistedRounds = testCase.proposed
			require.NoError(t, prev.VerifyProposedBlacklist(nodesWithWeights(weights), proposed, 5))
		})
	}
}

func TestVerifyProposedBlacklistHugeSuspectedNodesCost(t *testing.T) {
	prev := NewBlacklist(10)
	candidate := prev.Next(nodesWithWeights(equalWeights(4)), 5, 10, nil, nil)
	candidate.suspectedNodes = make(suspectedNodes, 1_000_000)

	// Round-trip through bytes, as the candidate would arrive over the wire.
	var decoded Blacklist
	require.NoError(t, decoded.FromBytes(candidate.Bytes()))
	require.Len(t, decoded.suspectedNodes, 1_000_000)

	err := prev.VerifyProposedBlacklist(nodesWithWeights(equalWeights(4)), decoded, 5)
	require.ErrorIs(t, err, errBlacklistTooManySuspectedNodes)
	require.Less(t, len(err.Error()), 200)
}

func TestBlacklistNextMaxBlacklistedRounds(t *testing.T) {
	const nodeCount = 4

	for _, testCase := range []struct {
		name     string
		current  uint16
		target   uint16
		expected uint16
	}{
		{name: "equal to target", current: 40, target: 40, expected: 40},
		{name: "less than an orbit below target", current: 40, target: 43, expected: 43},
		{name: "less than an orbit above target", current: 40, target: 37, expected: 37},
		{name: "one orbit below target", current: 40, target: 44, expected: 44},
		{name: "one orbit above target", current: 40, target: 36, expected: 36},
		{name: "far below target", current: 40, target: 100, expected: 44},
		{name: "far above target", current: 40, target: 10, expected: 36},
		{name: "never goes below node count", current: 6, target: 0, expected: 4},
		{name: "stays at node count", current: 4, target: 0, expected: 4},
		{name: "within an orbit of a target below node count", current: 5, target: 2, expected: 4},
		{name: "below node count is raised to node count", current: 1, target: 0, expected: 4},
		{name: "zero is raised to node count", current: 0, target: 100, expected: 4},
		{name: "reaches the maximum", current: math.MaxUint16 - nodeCount, target: math.MaxUint16, expected: math.MaxUint16},
		{name: "never overflows", current: math.MaxUint16, target: math.MaxUint16, expected: math.MaxUint16},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bl := Blacklist{maxBlacklistedRounds: testCase.current}
			next := bl.Next(nodesWithWeights(equalWeights(nodeCount)), 5, testCase.target, nil, nil)
			require.Equal(t, testCase.expected, next.maxBlacklistedRounds)
			require.True(t, isValidMaxBlacklistedRoundsTransition(testCase.current, next.maxBlacklistedRounds, nodeCount))
		})
	}
}

func TestBlacklistStateless(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		current   uint16
		nodeCount uint16
		expected  uint16
	}{
		{name: "max blacklisted rounds is carried over", current: 4620, nodeCount: 4, expected: 4620},
		{name: "max blacklisted rounds is raised to the node count", current: 8, nodeCount: 10, expected: 10},
		{name: "zero max blacklisted rounds starts from genesis", current: 0, nodeCount: 4, expected: GenesisMaxBlacklistedRounds},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bl := Blacklist{
				maxBlacklistedRounds: testCase.current,
				suspectedNodes:       suspectedNodes{{NodeIndex: 1, SuspectingWeight: 2, OrbitSuspected: 1}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 1}},
			}
			stateless := bl.Stateless(testCase.nodeCount)
			require.Equal(t, testCase.expected, stateless.maxBlacklistedRounds)
			require.Empty(t, stateless.suspectedNodes)
			require.Empty(t, stateless.updates)
		})
	}
}

func TestBlacklistMaxBlacklistedRoundsRollingUpgrade(t *testing.T) {
	// This test simulates a rolling upgrade where every node is reconfigured, one after another,
	// from oldTarget to newTarget orbits. Each leader moves the value towards its own target,
	// and every block must be accepted by all nodes regardless of their configuration.
	const (
		nodeCount = 7
		oldTarget = 5
		newTarget = 20
	)
	weights := equalWeights(nodeCount)

	targets := make([]uint16, nodeCount)
	for i := range targets {
		targets[i] = oldTarget
	}

	bl := Blacklist{maxBlacklistedRounds: oldTarget * nodeCount}
	round := uint64(0)

	buildAndVerify := func() {
		leader := round % nodeCount
		// The leader's target orbits correspond to that many orbits' worth of rounds.
		next := bl.Next(nodesWithWeights(weights), round, targets[leader]*nodeCount, nil, nil)
		require.NoError(t, bl.VerifyProposedBlacklist(nodesWithWeights(weights), next, round), "round %d", round)
		bl = next
		round++
	}

	// Upgrade the nodes one by one. While the upgraded nodes are a minority, the value can only
	// be pushed above the old target by the upgraded nodes that lead consecutively, and is then pulled back.
	// Once they are a majority, the value drifts towards the new target.
	for upgraded := 1; upgraded < nodeCount; upgraded++ {
		targets[upgraded-1] = newTarget
		for range 3 * nodeCount {
			buildAndVerify()
			require.GreaterOrEqual(t, bl.maxBlacklistedRounds, uint16(oldTarget*nodeCount), "round %d", round)
			require.LessOrEqual(t, bl.maxBlacklistedRounds, uint16(newTarget*nodeCount), "round %d", round)
			if upgraded <= nodeCount-upgraded {
				require.LessOrEqual(t, bl.maxBlacklistedRounds, uint16((oldTarget+upgraded)*nodeCount), "round %d", round)
			}
		}
	}

	// Once all nodes are upgraded, the value moves by one orbit per block until it reaches the new target.
	targets[nodeCount-1] = newTarget
	for range newTarget - oldTarget {
		buildAndVerify()
	}
	require.Equal(t, uint16(newTarget*nodeCount), bl.maxBlacklistedRounds)

	// And it stays there.
	for range 2 * nodeCount {
		buildAndVerify()
		require.Equal(t, uint16(newTarget*nodeCount), bl.maxBlacklistedRounds)
	}
}

// equalWeights returns the weights of nodeCount nodes that all have a weight of 1.
func equalWeights(nodeCount uint16) []uint64 {
	weights := make([]uint64, nodeCount)
	for i := range weights {
		weights[i] = 1
	}
	return weights
}

// nodesWithWeights returns Nodes with the given weights, one node per weight, for use with
// functions that take Nodes instead of a node count and a weights slice.
func nodesWithWeights(weights []uint64) Nodes {
	nodes := make(Nodes, len(weights))
	for i, w := range weights {
		nodes[i] = Node{Weight: w}
	}
	return nodes
}

func TestBlacklistEqualWeightsThresholdMatchesNodeCount(t *testing.T) {
	// With equal weights, the weighted threshold must be f+1 nodes, as it was when counting nodes.
	for nodeCount := uint16(1); nodeCount <= 100; nodeCount++ {
		var bl Blacklist
		require.Equal(t, uint64(F(int(nodeCount))+1), bl.computeThreshold(uint64(nodeCount)), "nodeCount %d", nodeCount)
	}
}

func TestBlacklistSuspectingWeightIsTheLeaderWeight(t *testing.T) {
	// Node 0 holds a threshold of the weight on its own, the rest of the nodes do not.
	weights := []uint64{10, 1, 1, 1}
	totalWeight := uint64s(weights).Sum()
	const suspectedNode = 2

	for _, testCase := range []struct {
		name                     string
		round                    uint64
		expectedSuspectingWeight uint64
		expectBlacklisted        bool
	}{
		{
			name:                     "heavy leader blacklists the node on its own",
			round:                    4, // leader is node 0
			expectedSuspectingWeight: 10,
			expectBlacklisted:        true,
		},
		{
			name:                     "light leader does not blacklist the node on its own",
			round:                    5, // leader is node 1
			expectedSuspectingWeight: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bl := NewBlacklist(GenesisMaxBlacklistedRounds)
			require.Equal(t, uint64(1), orbit(testCase.round, suspectedNode, 4))

			next := bl.Next(nodesWithWeights(weights), testCase.round, GenesisMaxBlacklistedRounds, map[uint16]uint64{suspectedNode: testCase.round}, nil)
			require.Equal(t, suspectedNodes{{NodeIndex: suspectedNode, SuspectingWeight: testCase.expectedSuspectingWeight, OrbitSuspected: 1}}, next.suspectedNodes)
			require.Equal(t, testCase.expectBlacklisted, next.IsNodeSuspected(suspectedNode, totalWeight))

			require.NoError(t, bl.VerifyProposedBlacklist(nodesWithWeights(weights), next, testCase.round))
		})
	}
}

func TestBlacklistLightLeadersAccumulateSuspectingWeight(t *testing.T) {
	// The total weight is 7, so the threshold is 3 and it takes three accusations of weight 1 within an orbit.
	weights := []uint64{2, 1, 1, 1, 1, 1}
	totalWeight := uint64s(weights).Sum()
	bl := NewBlacklist(GenesisMaxBlacklistedRounds)
	require.Equal(t, uint64(3), bl.computeThreshold(totalWeight))

	const suspectedNode = 5
	// Rounds 5 to 10 are all in orbit 1 of node 5.
	// Node 1 (weight 1) accuses, then node 2 (weight 1) accuses, then node 3 (weight 1) accuses.
	for i, round := range []uint64{7, 8, 9} {
		require.Equal(t, uint64(1), orbit(round, suspectedNode, 6))
		next := bl.Next(nodesWithWeights(weights), round, GenesisMaxBlacklistedRounds, map[uint16]uint64{suspectedNode: round}, nil)
		require.NoError(t, bl.VerifyProposedBlacklist(nodesWithWeights(weights), next, round))
		bl = next
		require.Equal(t, uint64(i+1), bl.suspectedNodes[0].SuspectingWeight)
		require.Equal(t, i == 2, bl.IsNodeSuspected(suspectedNode, totalWeight), "round %d", round)
	}
}

func TestBlacklistRedeemingWeightIsTheLeaderWeight(t *testing.T) {
	weights := []uint64{10, 1, 1, 1}
	totalWeight := uint64s(weights).Sum()
	const blacklistedNode = 2

	bl := NewBlacklist(GenesisMaxBlacklistedRounds)
	bl.suspectedNodes = suspectedNodes{{NodeIndex: blacklistedNode, SuspectingWeight: 5, OrbitSuspected: 1}}
	require.True(t, bl.IsNodeSuspected(blacklistedNode, totalWeight))

	redeem := func(round uint64) {
		require.Equal(t, uint64(2), orbit(round, blacklistedNode, 4))
		next := bl.Next(nodesWithWeights(weights), round, GenesisMaxBlacklistedRounds, nil, map[uint16]uint64{blacklistedNode: round})
		require.NoError(t, bl.VerifyProposedBlacklist(nodesWithWeights(weights), next, round))
		bl = next
	}

	// Node 3 (weight 1) redeems the node, which is not enough.
	redeem(7)
	require.True(t, bl.IsNodeSuspected(blacklistedNode, totalWeight))
	require.Equal(t, suspectedNodes{{NodeIndex: blacklistedNode, SuspectingWeight: 5, OrbitSuspected: 1, RedeemingWeight: 1, OrbitToRedeem: 2}}, bl.suspectedNodes)

	// Node 0 (weight 10) redeems the node, which pushes the redeeming weight over the threshold.
	redeem(8)
	require.False(t, bl.IsNodeSuspected(blacklistedNode, totalWeight))
	require.Empty(t, bl.suspectedNodes)
}

func TestBlacklistVerifyProposedBlacklistUsesWeights(t *testing.T) {
	// A blacklist built with one set of weights must not be accepted by a verifier that uses a different set of weights.
	weights := []uint64{10, 1, 1, 1}
	bl := NewBlacklist(GenesisMaxBlacklistedRounds)

	next := bl.Next(nodesWithWeights(weights), 4, GenesisMaxBlacklistedRounds, map[uint16]uint64{2: 4}, nil)
	require.NoError(t, bl.VerifyProposedBlacklist(nodesWithWeights(weights), next, 4))

	otherWeights := []uint64{1, 10, 1, 1}
	err := bl.VerifyProposedBlacklist(nodesWithWeights(otherWeights), next, 4)
	require.ErrorContains(t, err, errBlacklistInvalidBlacklist.Error())
}

func TestBlacklistCapIsWeightBased(t *testing.T) {
	t.Run("equal weights allow up to f blacklisted nodes", func(t *testing.T) {
		const nodeCount = 10
		weights := equalWeights(nodeCount)
		bl := Blacklist{maxBlacklistedRounds: nodeCount}
		threshold := bl.computeThreshold(nodeCount)

		bl.suspectedNodes = suspectedNodes{
			{NodeIndex: 0, SuspectingWeight: threshold, OrbitSuspected: 1},
			{NodeIndex: 1, SuspectingWeight: threshold, OrbitSuspected: 1},
		}
		bl.setNodeSuspected(1, 2, weights[9], weights, nodeCount)
		require.Len(t, bl.suspectedNodes, 3, "a third node can still be accused")
		bl.suspectedNodes[2].SuspectingWeight = threshold

		bl.setNodeSuspected(1, 3, weights[9], weights, nodeCount)
		require.Len(t, bl.suspectedNodes, 3, "f=3 nodes are blacklisted, a fourth cannot be accused")
	})

	t.Run("a heavy node cannot be blacklisted", func(t *testing.T) {
		// Node 0 holds half of the weight, more than the threshold of 4 out of 10.
		weights := []uint64{5, 1, 1, 1, 1, 1}
		totalWeight := uint64s(weights).Sum()
		bl := Blacklist{maxBlacklistedRounds: 6}

		bl.setNodeSuspected(1, 0, weights[1], weights, totalWeight)
		require.Empty(t, bl.suspectedNodes)

		bl.setNodeSuspected(1, 2, weights[1], weights, totalWeight)
		require.Len(t, bl.suspectedNodes, 1, "a light node can still be accused")
	})

	t.Run("the weight of the accused node counts towards the cap", func(t *testing.T) {
		// The threshold is 4 out of 10, and node 1 (weight 1) is already blacklisted.
		weights := []uint64{3, 1, 1, 1, 1, 1, 1, 1}
		totalWeight := uint64s(weights).Sum()
		bl := Blacklist{maxBlacklistedRounds: 8}
		bl.suspectedNodes = suspectedNodes{{NodeIndex: 1, SuspectingWeight: 4, OrbitSuspected: 1}}
		require.True(t, bl.IsNodeSuspected(1, totalWeight))
		require.Len(t, bl.suspectedNodes, 1)

		// Blacklisting node 0 (weight 3) would put a threshold of the weight on the blacklist.
		bl.setNodeSuspected(1, 0, weights[3], weights, totalWeight)
		require.Len(t, bl.suspectedNodes, 1)
		require.False(t, bl.IsNodeSuspected(0, totalWeight))

		// Node 2 (weight 1) can still be accused.
		bl.setNodeSuspected(1, 2, weights[3], weights, totalWeight)
		bl.setNodeSuspected(1, 2, weights[4], weights, totalWeight)
		bl.setNodeSuspected(1, 2, weights[5], weights, totalWeight)
		bl.setNodeSuspected(1, 2, weights[6], weights, totalWeight)
		require.Equal(t, suspectedNodes{
			{NodeIndex: 1, SuspectingWeight: 4, OrbitSuspected: 1},
			{NodeIndex: 2, SuspectingWeight: 4, OrbitSuspected: 1},
		}, bl.suspectedNodes)
		require.True(t, bl.IsNodeSuspected(1, totalWeight))
		require.True(t, bl.IsNodeSuspected(2, totalWeight))
	})

	t.Run("only blacklisted nodes count towards the cap", func(t *testing.T) {
		// Node 1 is accused but not blacklisted, so its weight does not count towards the cap.
		weights := []uint64{3, 1, 1, 1, 1, 1, 1, 1}
		bl := Blacklist{maxBlacklistedRounds: 8}
		bl.suspectedNodes = suspectedNodes{{NodeIndex: 1, SuspectingWeight: 3, OrbitSuspected: 1}}
		require.False(t, bl.IsNodeSuspected(1, uint64s(weights).Sum()))
	})
}

func TestBlacklistIgnoresMismatchingWeights(t *testing.T) {
	const (
		nodeCount   = 4
		accusedNode = 2
		// Round 3 is in orbit 1 of node 2, and round 7 is in orbit 2 of node 2.
		suspectRound  = 3
		suspectOrbit  = 1
		redeemRound   = 7
		redeemOrbit   = 2
		blacklistedAt = suspectOrbit
	)

	suspect := []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: accusedNode}}
	redeem := []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: accusedNode}}

	// blacklisted is the accused node after it has reached the threshold of 2.
	blacklisted := suspectedNodes{{NodeIndex: accusedNode, SuspectingWeight: 2, OrbitSuspected: blacklistedAt}}

	for _, testCase := range []struct {
		name                   string
		suspectedNodes         suspectedNodes
		updates                []BlacklistUpdate
		round                  uint64
		weights                []uint64
		expectedSuspectedNodes suspectedNodes
	}{
		{
			// Control case: the same call with correct weights does change the blacklist,
			// so the no-ops below are caused by the weights alone.
			name:                   "suspect with one weight per node is applied",
			updates:                suspect,
			round:                  suspectRound,
			weights:                equalWeights(nodeCount),
			expectedSuspectedNodes: suspectedNodes{{NodeIndex: accusedNode, SuspectingWeight: 1, OrbitSuspected: suspectOrbit}},
		},
		{
			name:                   "suspect with fewer weights than nodes is ignored",
			updates:                suspect,
			round:                  suspectRound,
			weights:                equalWeights(nodeCount - 1),
			expectedSuspectedNodes: suspectedNodes{},
		},
		{
			// Indexing into the weights would not fail here, so this is caught only by checking the length.
			name:                   "suspect with more weights than nodes is ignored",
			updates:                suspect,
			round:                  suspectRound,
			weights:                equalWeights(nodeCount + 1),
			expectedSuspectedNodes: suspectedNodes{},
		},
		{
			name:                   "suspect of an out of range node index is ignored",
			updates:                []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: nodeCount}},
			round:                  suspectRound,
			weights:                equalWeights(nodeCount),
			expectedSuspectedNodes: suspectedNodes{},
		},
		{
			// Control case: the same call with correct weights adds the leader's weight to the redeeming weight.
			name:                   "redeem with one weight per node is applied",
			suspectedNodes:         blacklisted,
			updates:                redeem,
			round:                  redeemRound,
			weights:                equalWeights(nodeCount),
			expectedSuspectedNodes: suspectedNodes{{NodeIndex: accusedNode, SuspectingWeight: 2, OrbitSuspected: blacklistedAt, RedeemingWeight: 1, OrbitToRedeem: redeemOrbit}},
		},
		{
			name:                   "redeem with fewer weights than nodes is ignored",
			suspectedNodes:         blacklisted,
			updates:                redeem,
			round:                  redeemRound,
			weights:                equalWeights(nodeCount - 1),
			expectedSuspectedNodes: blacklisted,
		},
		{
			name:                   "redeem with more weights than nodes is ignored",
			suspectedNodes:         blacklisted,
			updates:                redeem,
			round:                  redeemRound,
			weights:                equalWeights(nodeCount + 1),
			expectedSuspectedNodes: blacklisted,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// The accused node must not expire between the orbit it is suspected in and the orbit it is redeemed in.
			bl := Blacklist{
				maxBlacklistedRounds: 10 * nodeCount,
				suspectedNodes:       testCase.suspectedNodes,
			}
			next := bl.applyUpdates(nodeCount, testCase.updates, testCase.round, testCase.weights, uint64s(testCase.weights).Sum())
			require.Equal(t, testCase.expectedSuspectedNodes, next.suspectedNodes)
		})
	}
}

func TestBlacklistThresholdUsesGivenTotalWeight(t *testing.T) {
	bl := Blacklist{
		maxBlacklistedRounds: GenesisMaxBlacklistedRounds,
		suspectedNodes:       suspectedNodes{{NodeIndex: 2, SuspectingWeight: 5, OrbitSuspected: 1}},
	}

	// A total weight of 13 has a threshold of 5.
	require.True(t, bl.IsNodeSuspected(2, 13))
	// A total weight of 20 has a threshold of 7.
	require.False(t, bl.IsNodeSuspected(2, 20))

	// Serialization does not depend on the total weight.
	var decoded Blacklist
	require.NoError(t, decoded.FromBytes(bl.Bytes()))
	require.True(t, bl.Equals(&decoded))
}

func TestBlacklistSimulateWeightedNetwork(t *testing.T) {
	for _, nodes := range []int{4, 7, 16, 64} {
		t.Run(fmt.Sprintf("n=%d", nodes), func(t *testing.T) {
			t.Parallel()
			weights := make([]uint64, nodes)
			for i := range weights {
				weights[i] = uint64(mathrand.Intn(100) + 1)
			}
			testBlacklistSimulateNetwork(t, weights, 1000)
		})
	}
}
