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
		nodeCount         int
		round             uint64
		expectedErr       error
	}{
		{
			name:      "node count mismatch",
			nodeCount: 5,
			proposedBlacklist: Blacklist{
				nodeCount:   13,
				totalWeight: 13,
			},
			expectedErr: errBlacklistInvalidNodeCount,
		},
		{
			name: "total weight mismatch",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          1,
				maxBlacklistedOrbits: 1,
			},
			expectedErr: errBlacklistInvalidWeightCount,
		},
		{
			name: "max blacklisted orbits increased by more than one",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 10,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 12,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedOrbits,
		},
		{
			name: "max blacklisted orbits decreased by more than one",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 10,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 8,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedOrbits,
		},
		{
			name: "max blacklisted orbits decreased to zero",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 0,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedOrbits,
		},
		{
			name: "max blacklisted orbits overflows",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: math.MaxUint16,
			},
			nodeCount: 4,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 0,
			},
			expectedErr: errBlacklistInvalidMaxBlacklistedOrbits,
		},
		{
			name: "invalid blacklist update",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 0}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 0}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 3}},
			},
			expectedErr: errBlacklistInvalidUpdates,
		},
		{
			name: "node redeemed in the same orbit it was suspected in",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 7, RedeemingWeight: 0, OrbitToRedeem: 0}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 7, RedeemingWeight: 1, OrbitToRedeem: 7}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3}},
			},
			expectedErr: errBlacklistInvalidBlacklist,
		},
		{
			name: "blacklist mismatch",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 0}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2, OrbitSuspected: 6, RedeemingWeight: 0, OrbitToRedeem: 7}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3}},
			},
			expectedErr: errBlacklistInvalidBlacklist,
		},
		{
			name: "blacklist mismatch - too many blacklisted",
			blacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}, {NodeIndex: 2, SuspectingWeight: 2}},
			},
			nodeCount: 4,
			round:     29,
			proposedBlacklist: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
				suspectedNodes:       suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}, {NodeIndex: 2, SuspectingWeight: 2}, {NodeIndex: 1, SuspectingWeight: 1}},
				updates:              []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 1}},
			},
			expectedErr: errBlacklistInvalidBlacklist,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.blacklist.VerifyProposedBlacklist(testCase.proposedBlacklist, testCase.round, equalWeights(testCase.blacklist.nodeCount))
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
		nodeCount:            4,
		totalWeight:          4,
		maxBlacklistedOrbits: 7,
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
			data:              (&Blacklist{nodeCount: 100, totalWeight: 100}).Bytes(),
			expectedBlacklist: Blacklist{nodeCount: 100, totalWeight: 100},
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
	bl := Blacklist{
		nodeCount:   4,
		totalWeight: 4,
	}
	require.False(t, bl.IsNodeSuspected(2))

	bl.setNodeSuspected(1, 2, 1, equalWeights(bl.nodeCount))
	require.False(t, bl.IsNodeSuspected(2))

	bl.setNodeSuspected(1, 2, 1, equalWeights(bl.nodeCount))
	require.True(t, bl.IsNodeSuspected(2))

	bl.suspectedNodes = bl.garbageCollectSuspectedNodes(5)
	require.True(t, bl.IsNodeSuspected(2), "Suspected node is still suspected even if its OrbitSuspected advanced")

	bl.setNodeRedeemed(3, 2, 1)
	require.True(t, bl.IsNodeSuspected(2))

	bl.setNodeRedeemed(3, 2, 1)
	require.False(t, bl.IsNodeSuspected(2))
}

// TestBlacklistCapAppliesToPendingSuspects asserts that at most f nodes can be blacklisted.
func TestBlacklistCapAppliesToPendingSuspects(t *testing.T) {
	const nodeCount = 4
	f := (nodeCount - 1) / 3
	require.Equal(t, 1, f)

	countBlacklisted := func(bl Blacklist) int {
		blacklisted := 0
		for node := uint16(0); node < nodeCount; node++ {
			if bl.IsNodeSuspected(node) {
				blacklisted++
			}
		}
		return blacklisted
	}

	t.Run("setNodeSuspected", func(t *testing.T) {
		bl := Blacklist{nodeCount: nodeCount, totalWeight: nodeCount}

		// One accusation each: both nodes are pending, neither is blacklisted.
		bl.setNodeSuspected(1, 2, 1, equalWeights(bl.nodeCount))
		bl.setNodeSuspected(1, 3, 1, equalWeights(bl.nodeCount))
		require.Len(t, bl.suspectedNodes, 2)
		require.Zero(t, countBlacklisted(bl))

		// A second accusation blacklists node 2 and fills the cap.
		bl.setNodeSuspected(1, 2, 1, equalWeights(bl.nodeCount))
		require.True(t, bl.IsNodeSuspected(2))

		// A second accusation of the pending node 3 must not blacklist it: f nodes are already blacklisted.
		bl.setNodeSuspected(1, 3, 1, equalWeights(bl.nodeCount))
		require.False(t, bl.IsNodeSuspected(3))
		require.LessOrEqual(t, countBlacklisted(bl), f, "%s", bl.String())
	})

	t.Run("Next", func(t *testing.T) {
		// Two consecutive rounds within one orbit for both accused nodes, as two successive leaders
		// would produce when accusing the same pair.
		const firstRound, secondRound = 2, 3
		for _, node := range []uint16{0, 1} {
			require.Equal(t, orbit(firstRound, node, nodeCount), orbit(secondRound, node, nodeCount))
		}

		bl := Blacklist{nodeCount: nodeCount, totalWeight: nodeCount}
		bl = bl.Next(firstRound, 0, equalWeights(bl.nodeCount), map[uint16]uint64{0: firstRound, 1: firstRound}, nil)
		require.Len(t, bl.suspectedNodes, 2)
		require.Zero(t, countBlacklisted(bl))

		bl = bl.Next(secondRound, 0, equalWeights(bl.nodeCount), map[uint16]uint64{0: secondRound, 1: secondRound}, nil)
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
		nodeCount:   5,
		totalWeight: 5,
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
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: testCase.suspectedNodes,
			}

			next := blacklist.Next(testCase.round, 0, equalWeights(blacklist.nodeCount), testCase.timedOut, testCase.redeemed)
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
			blacklist := Blacklist{
				nodeCount:            uint16(len(nodes)),
				totalWeight:          uint64(len(nodes)),
				maxBlacklistedOrbits: 10,
				suspectedNodes:       suspectedNodesBefore,
			}

			suspectingNodes := blacklist.garbageCollectSuspectedNodes(round)

			if testCases[round].noChange {
				require.Equal(t, suspectedNodesBefore, suspectingNodes)
			} else {
				require.Equal(t, testCases[round].suspectedNodesAfter, suspectingNodes)
			}
		})
	}
}

func TestVerifyBlacklistUpdates(t *testing.T) {
	testBlacklist := Blacklist{
		nodeCount:   4,
		totalWeight: 4,
	}

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
			Blacklist:   testBlacklist,
		},
		{
			name: "invalid index",
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 4},
			},
			expectedErr: errBlacklistInvalidNodeIndex,
			Blacklist:   testBlacklist,
		},
		{
			name: "double vote",
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 3},
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2},
				{Type: BlacklistOpType_NodeSuspected, NodeIndex: 3},
			},
			expectedErr: errBlacklistNodeIndexAlreadyUpdated,
			Blacklist:   testBlacklist,
		},
		{
			name: "already blacklisted",
			Blacklist: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
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
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: []SuspectedNode{{NodeIndex: 3, SuspectingWeight: 1}},
			},
			updates: []BlacklistUpdate{
				{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3},
			},
			expectedErr: errBlacklistNodeNotBlacklisted,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.Blacklist.verifyBlacklistUpdates(testCase.updates)
			require.ErrorContains(t, err, testCase.expectedErr.Error())
		})
	}
}

func TestBlacklistMultipleAttemptsOverSeveralOrbits(t *testing.T) {
	// This test tests that even though a node is suspected continuously across several rounds,
	// as long as the rounds are too far from each other, the node will not be blacklisted.

	blacklist := Blacklist{
		nodeCount:   4,
		totalWeight: 4,
	}

	blacklist = blacklist.Next(4, 0, equalWeights(blacklist.nodeCount), map[uint16]uint64{3: 4}, nil)
	require.False(t, blacklist.IsNodeSuspected(3))

	blacklist = blacklist.Next(7, 0, equalWeights(blacklist.nodeCount), map[uint16]uint64{3: 7}, nil)
	require.False(t, blacklist.IsNodeSuspected(3))

	blacklist = blacklist.Next(8, 0, equalWeights(blacklist.nodeCount), map[uint16]uint64{3: 8}, nil)
	require.True(t, blacklist.IsNodeSuspected(3))
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
	blacklist.nodeCount = uint16(nodeCount)
	blacklist.totalWeight = uint64s(weights).Sum()
	blacklist.maxBlacklistedOrbits = 1

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

	leader := uint16(round % uint64(nodeCount))
	if _, leaderCrashed := crashedNodes[leader]; leaderCrashed {
		// leader is crashed, nothing further that can be done.
		newBlacklist := Blacklist{
			nodeCount:            prevBlacklist.nodeCount,
			totalWeight:          prevBlacklist.totalWeight,
			maxBlacklistedOrbits: prevBlacklist.maxBlacklistedOrbits,
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

	// The blacklist starts with a single max blacklisted orbit, which is the target for zero max blacklisted rounds.
	newBlacklist := prevBlacklist.Next(round, 0, weights, timedOut, redeemed)

	err := prevBlacklist.VerifyProposedBlacklist(newBlacklist, round, weights)
	require.NoError(t, err, "round %d", round)

	// The blacklisted nodes must never hold a threshold of the total weight.
	var blacklistedWeight uint64
	for node := uint16(0); node < uint16(nodeCount); node++ {
		if newBlacklist.IsNodeSuspected(node) {
			blacklistedWeight += weights[node]
		}
	}
	require.Less(t, blacklistedWeight, newBlacklist.computeThreshold(), "round %d: %s", round, newBlacklist.String())

	return newBlacklist
}

func TestBlacklistEquals(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		bl1, bl2 Blacklist
		equals   bool
	}{
		{
			name: "different node count",
			bl1: Blacklist{
				nodeCount:   4,
				totalWeight: 4,
			},
			bl2: Blacklist{
				nodeCount:   5,
				totalWeight: 5,
			},
		},
		{
			name: "different total weight",
			bl1: Blacklist{
				nodeCount:   4,
				totalWeight: 4,
			},
			bl2: Blacklist{
				nodeCount:   4,
				totalWeight: 40,
			},
		},
		{
			name: "different max blacklisted orbits",
			bl1: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 1,
			},
			bl2: Blacklist{
				nodeCount:            4,
				totalWeight:          4,
				maxBlacklistedOrbits: 2,
			},
		},
		{
			name: "different suspected nodes count",
			bl1: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
			},
			bl2: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}, {NodeIndex: 2, SuspectingWeight: 1}},
			},
		},
		{
			name: "different suspected nodes content",
			bl1: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
			},
			bl2: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 1}},
			},
		},
		{
			name: "different updates count",
			bl1: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			bl2: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}, {Type: BlacklistOpType_NodeRedeemed, NodeIndex: 3}},
			},
		},
		{
			name: "different updates content",
			bl1: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			bl2: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: 2}},
			},
		},
		{
			name: "equal blacklists",
			bl1: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			bl2: Blacklist{
				nodeCount:      4,
				totalWeight:    4,
				suspectedNodes: suspectedNodes{{NodeIndex: 3, SuspectingWeight: 2}},
				updates:        []BlacklistUpdate{{Type: BlacklistOpType_NodeSuspected, NodeIndex: 2}},
			},
			equals: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.equals, testCase.bl1.Equals(&testCase.bl2))
		})
	}
}

func TestNewBlacklistMaxBlacklistedOrbits(t *testing.T) {
	for _, testCase := range []struct {
		name                 string
		nodeCount            uint16
		maxBlacklistedRounds uint16
		expectedOrbits       uint16
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
			name:                 "fewer rounds than nodes yields a single orbit",
			nodeCount:            10,
			maxBlacklistedRounds: 3,
			expectedOrbits:       1,
		},
		{
			name:                 "zero rounds yields a single orbit",
			nodeCount:            4,
			maxBlacklistedRounds: 0,
			expectedOrbits:       1,
		},
		{
			name:                 "zero node count does not divide by zero",
			nodeCount:            0,
			maxBlacklistedRounds: 9,
			expectedOrbits:       9,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bl := NewBlacklist(testCase.nodeCount, uint64(testCase.nodeCount), testCase.maxBlacklistedRounds)
			require.Equal(t, testCase.nodeCount, bl.nodeCount)
			require.Equal(t, testCase.expectedOrbits, bl.maxBlacklistedOrbits)
		})
	}
}

func TestBlacklistMaxBlacklistedOrbitsIsPreserved(t *testing.T) {
	bl := NewBlacklist(4, uint64(4), 4620)
	require.Equal(t, uint16(1155), bl.maxBlacklistedOrbits)

	applied := bl.Next(5, 4620, equalWeights(bl.nodeCount), map[uint16]uint64{1: 5}, nil)
	require.Equal(t, bl.maxBlacklistedOrbits, applied.maxBlacklistedOrbits, "Next should carry over maxBlacklistedOrbits when it is already at the target")

	cloned := applied.Clone()
	require.Equal(t, bl.maxBlacklistedOrbits, cloned.maxBlacklistedOrbits, "Clone should carry over maxBlacklistedOrbits")
	require.True(t, applied.Equals(&cloned))

	var decoded Blacklist
	require.NoError(t, decoded.FromBytes(applied.Bytes()))
	require.Equal(t, bl.maxBlacklistedOrbits, decoded.maxBlacklistedOrbits, "serialization should preserve maxBlacklistedOrbits")
	require.True(t, applied.Equals(&decoded))
}

func TestBlacklistedNodeExpiresAfterMaxBlacklistedOrbits(t *testing.T) {
	const (
		nodeCount            = 4
		maxBlacklistedOrbits = 2
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
				nodeCount:            nodeCount,
				totalWeight:          nodeCount,
				maxBlacklistedOrbits: maxBlacklistedOrbits,
				suspectedNodes:       suspectedNodes{testCase.suspectedNode},
			}
			require.True(t, bl.IsNodeSuspected(blacklistedNode))

			next := bl.Next(testCase.round, nodeCount*maxBlacklistedOrbits, equalWeights(bl.nodeCount), nil, nil)
			require.Equal(t, uint16(maxBlacklistedOrbits), next.maxBlacklistedOrbits)

			if testCase.expectExpired {
				require.False(t, next.IsNodeSuspected(blacklistedNode))
				require.Empty(t, next.suspectedNodes)
			} else {
				require.True(t, next.IsNodeSuspected(blacklistedNode))
				require.Len(t, next.suspectedNodes, 1)
			}

			// The blacklist of the previous round must not be modified.
			require.True(t, bl.IsNodeSuspected(blacklistedNode))
		})
	}
}

func TestBlacklistedNodeExpiryEndToEnd(t *testing.T) {
	const (
		nodeCount            = 4
		maxBlacklistedRounds = 8 // 2 orbits
		blacklistedNode      = 1
	)

	bl := NewBlacklist(nodeCount, uint64(nodeCount), maxBlacklistedRounds)
	require.Equal(t, uint16(2), bl.maxBlacklistedOrbits)

	// suspect makes the leader of the given round accuse the blacklisted node of timing out in that round.
	suspect := func(round uint64) map[uint16]uint64 {
		return map[uint16]uint64{blacklistedNode: round}
	}

	// Round 2 and 3 are in orbit 1 for node 1. Two accusations blacklist it (f+1 = 2).
	bl = bl.Next(2, maxBlacklistedRounds, equalWeights(bl.nodeCount), suspect(2), nil)
	require.False(t, bl.IsNodeSuspected(blacklistedNode))
	bl = bl.Next(3, maxBlacklistedRounds, equalWeights(bl.nodeCount), suspect(3), nil)
	require.True(t, bl.IsNodeSuspected(blacklistedNode))

	// Round 4 is still orbit 1 for node 1, and orbit 2 spans rounds 5-8: still blacklisted.
	for round := uint64(4); round <= 8; round++ {
		bl = bl.Next(round, maxBlacklistedRounds, equalWeights(bl.nodeCount), nil, nil)
		require.True(t, bl.IsNodeSuspected(blacklistedNode), "round %d", round)
	}

	// orbit 3 (round 9): expired.
	bl = bl.Next(9, maxBlacklistedRounds, equalWeights(bl.nodeCount), nil, nil)
	require.False(t, bl.IsNodeSuspected(blacklistedNode))
	require.Empty(t, bl.suspectedNodes)

	// A redeem update for an expired node is rejected, as the node is no longer blacklisted.
	redeem := []BlacklistUpdate{{Type: BlacklistOpType_NodeRedeemed, NodeIndex: blacklistedNode}}
	require.ErrorContains(t, bl.verifyBlacklistUpdates(redeem), errBlacklistNodeNotBlacklisted.Error())

	// An expired node can be suspected again from scratch.
	bl = bl.Next(10, maxBlacklistedRounds, equalWeights(bl.nodeCount), suspect(10), nil)
	require.False(t, bl.IsNodeSuspected(blacklistedNode))
	require.Equal(t, suspectedNodes{{NodeIndex: blacklistedNode, SuspectingWeight: 1, OrbitSuspected: 3}}, bl.suspectedNodes)
}

func TestBlacklistVerifyProposedBlacklistMaxBlacklistedOrbitsStep(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		prev     uint16
		proposed uint16
	}{
		{name: "unchanged", prev: 10, proposed: 10},
		{name: "increased by one", prev: 10, proposed: 11},
		{name: "decreased by one", prev: 10, proposed: 9},
		{name: "decreased to the minimum", prev: 2, proposed: 1},
		{name: "increased to the maximum", prev: math.MaxUint16 - 1, proposed: math.MaxUint16},
		{name: "increased from zero", prev: 0, proposed: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			prev := Blacklist{nodeCount: 4, totalWeight: 4, maxBlacklistedOrbits: testCase.prev}
			proposed := prev.Next(5, 0, equalWeights(prev.nodeCount), nil, nil)
			proposed.maxBlacklistedOrbits = testCase.proposed
			require.NoError(t, prev.VerifyProposedBlacklist(proposed, 5, equalWeights(prev.nodeCount)))
		})
	}
}

func TestVerifyProposedBlacklistHugeSuspectedNodesCost(t *testing.T) {
	prev := NewBlacklist(4, uint64(4), 10)
	candidate := prev.Next(5, 10, equalWeights(prev.nodeCount), nil, nil)
	candidate.suspectedNodes = make(suspectedNodes, 1_000_000)

	// Round-trip through bytes, as the candidate would arrive over the wire.
	var decoded Blacklist
	require.NoError(t, decoded.FromBytes(candidate.Bytes()))
	require.Len(t, decoded.suspectedNodes, 1_000_000)

	err := prev.VerifyProposedBlacklist(decoded, 5, equalWeights(prev.nodeCount))
	require.ErrorIs(t, err, errBlacklistTooManySuspectedNodes)
	require.Less(t, len(err.Error()), 200)
}

func TestBlacklistNextMaxBlacklistedOrbits(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		current  uint16
		target   uint16
		expected uint16
	}{
		{name: "equal to target", current: 10, target: 10, expected: 10},
		{name: "below target", current: 10, target: 20, expected: 11},
		{name: "above target", current: 10, target: 5, expected: 9},
		{name: "never goes below one", current: 1, target: 0, expected: 1},
		{name: "never overflows", current: math.MaxUint16, target: math.MaxUint16, expected: math.MaxUint16},
		{name: "zero moves up to target", current: 0, target: 3, expected: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// With a single node, an orbit is a single round, so the max blacklisted rounds are the target orbits.
			// A target of zero rounds is treated as a single orbit.
			bl := Blacklist{nodeCount: 1, totalWeight: 1, maxBlacklistedOrbits: testCase.current}
			next := bl.Next(5, testCase.target, equalWeights(bl.nodeCount), nil, nil)
			require.Equal(t, testCase.expected, next.maxBlacklistedOrbits)
		})
	}
}

func TestRescaleMaxBlacklistedOrbits(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		orbits        uint16
		prevNodeCount uint16
		nodeCount     uint16
		expected      uint16
	}{
		{name: "same node count", orbits: 1155, prevNodeCount: 4, nodeCount: 4, expected: 1155},
		{name: "node count grows", orbits: 1155, prevNodeCount: 4, nodeCount: 64, expected: 72},
		{name: "node count shrinks", orbits: 72, prevNodeCount: 64, nodeCount: 4, expected: 1152},
		{name: "rounds down", orbits: 10, prevNodeCount: 4, nodeCount: 7, expected: 5},
		{name: "never goes below one", orbits: 1, prevNodeCount: 4, nodeCount: 100, expected: 1},
		{name: "never overflows", orbits: math.MaxUint16, prevNodeCount: 100, nodeCount: 4, expected: math.MaxUint16},
		{name: "unknown previous node count", orbits: 10, prevNodeCount: 0, nodeCount: 4, expected: 10},
		{name: "zero node count", orbits: 10, prevNodeCount: 4, nodeCount: 0, expected: 10},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, rescaleMaxBlacklistedOrbits(testCase.orbits, testCase.prevNodeCount, testCase.nodeCount))
		})
	}
}

func TestBlacklistMaxBlacklistedOrbitsRollingUpgrade(t *testing.T) {
	// This test simulates a rolling upgrade where every node is reconfigured, one after another,
	// from oldTarget to newTarget orbits. Each leader moves the value towards its own target,
	// and every block must be accepted by all nodes regardless of their configuration.
	const (
		nodeCount = 7
		oldTarget = 5
		newTarget = 20
	)

	targets := make([]uint16, nodeCount)
	for i := range targets {
		targets[i] = oldTarget
	}

	bl := Blacklist{nodeCount: nodeCount, totalWeight: nodeCount, maxBlacklistedOrbits: oldTarget}
	round := uint64(0)

	buildAndVerify := func() {
		leader := round % nodeCount
		// The leader's target orbits correspond to that many orbits' worth of rounds.
		next := bl.Next(round, targets[leader]*nodeCount, equalWeights(bl.nodeCount), nil, nil)
		require.NoError(t, bl.VerifyProposedBlacklist(next, round, equalWeights(bl.nodeCount)), "round %d", round)
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
			require.GreaterOrEqual(t, bl.maxBlacklistedOrbits, uint16(oldTarget), "round %d", round)
			require.LessOrEqual(t, bl.maxBlacklistedOrbits, uint16(newTarget), "round %d", round)
			if upgraded <= nodeCount-upgraded {
				require.LessOrEqual(t, bl.maxBlacklistedOrbits, uint16(oldTarget+upgraded), "round %d", round)
			}
		}
	}

	// Once all nodes are upgraded, the value moves by one orbit per block until it reaches the new target.
	targets[nodeCount-1] = newTarget
	for range newTarget - oldTarget {
		buildAndVerify()
	}
	require.Equal(t, uint16(newTarget), bl.maxBlacklistedOrbits)

	// And it stays there.
	for range 2 * nodeCount {
		buildAndVerify()
		require.Equal(t, uint16(newTarget), bl.maxBlacklistedOrbits)
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

func TestBlacklistEqualWeightsThresholdMatchesNodeCount(t *testing.T) {
	// With equal weights, the weighted threshold must be f+1 nodes, as it was when counting nodes.
	for nodeCount := uint16(1); nodeCount <= 100; nodeCount++ {
		bl := Blacklist{nodeCount: nodeCount, totalWeight: uint64(nodeCount)}
		require.Equal(t, uint64(F(int(nodeCount))+1), bl.computeThreshold(), "nodeCount %d", nodeCount)
	}
}

func TestBlacklistSuspectingWeightIsTheLeaderWeight(t *testing.T) {
	// Node 0 holds a threshold of the weight on its own, the rest of the nodes do not.
	weights := []uint64{10, 1, 1, 1}
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
			bl := NewBlacklist(4, uint64s(weights).Sum(), GenesisMaxBlacklistedRounds)
			require.Equal(t, uint64(1), orbit(testCase.round, suspectedNode, 4))

			next := bl.Next(testCase.round, GenesisMaxBlacklistedRounds, weights, map[uint16]uint64{suspectedNode: testCase.round}, nil)
			require.Equal(t, suspectedNodes{{NodeIndex: suspectedNode, SuspectingWeight: testCase.expectedSuspectingWeight, OrbitSuspected: 1}}, next.suspectedNodes)
			require.Equal(t, testCase.expectBlacklisted, next.IsNodeSuspected(suspectedNode))

			require.NoError(t, bl.VerifyProposedBlacklist(next, testCase.round, weights))
		})
	}
}

func TestBlacklistLightLeadersAccumulateSuspectingWeight(t *testing.T) {
	// The total weight is 7, so the threshold is 3 and it takes three accusations of weight 1 within an orbit.
	weights := []uint64{2, 1, 1, 1, 1, 1}
	bl := NewBlacklist(6, uint64s(weights).Sum(), GenesisMaxBlacklistedRounds)
	require.Equal(t, uint64(3), bl.computeThreshold())

	const suspectedNode = 5
	// Rounds 5 to 10 are all in orbit 1 of node 5.
	// Node 1 (weight 1) accuses, then node 2 (weight 1) accuses, then node 3 (weight 1) accuses.
	for i, round := range []uint64{7, 8, 9} {
		require.Equal(t, uint64(1), orbit(round, suspectedNode, 6))
		next := bl.Next(round, GenesisMaxBlacklistedRounds, weights, map[uint16]uint64{suspectedNode: round}, nil)
		require.NoError(t, bl.VerifyProposedBlacklist(next, round, weights))
		bl = next
		require.Equal(t, uint64(i+1), bl.suspectedNodes[0].SuspectingWeight)
		require.Equal(t, i == 2, bl.IsNodeSuspected(suspectedNode), "round %d", round)
	}
}

func TestBlacklistRedeemingWeightIsTheLeaderWeight(t *testing.T) {
	weights := []uint64{10, 1, 1, 1}
	const blacklistedNode = 2

	bl := NewBlacklist(4, uint64s(weights).Sum(), GenesisMaxBlacklistedRounds)
	bl.suspectedNodes = suspectedNodes{{NodeIndex: blacklistedNode, SuspectingWeight: 5, OrbitSuspected: 1}}
	require.True(t, bl.IsNodeSuspected(blacklistedNode))

	redeem := func(round uint64) {
		require.Equal(t, uint64(2), orbit(round, blacklistedNode, 4))
		next := bl.Next(round, GenesisMaxBlacklistedRounds, weights, nil, map[uint16]uint64{blacklistedNode: round})
		require.NoError(t, bl.VerifyProposedBlacklist(next, round, weights))
		bl = next
	}

	// Node 3 (weight 1) redeems the node, which is not enough.
	redeem(7)
	require.True(t, bl.IsNodeSuspected(blacklistedNode))
	require.Equal(t, suspectedNodes{{NodeIndex: blacklistedNode, SuspectingWeight: 5, OrbitSuspected: 1, RedeemingWeight: 1, OrbitToRedeem: 2}}, bl.suspectedNodes)

	// Node 0 (weight 10) redeems the node, which pushes the redeeming weight over the threshold.
	redeem(8)
	require.False(t, bl.IsNodeSuspected(blacklistedNode))
	require.Empty(t, bl.suspectedNodes)
}

func TestBlacklistVerifyProposedBlacklistUsesWeights(t *testing.T) {
	// A blacklist built with one set of weights must not be accepted by a verifier that uses a different set of weights.
	weights := []uint64{10, 1, 1, 1}
	bl := NewBlacklist(4, uint64s(weights).Sum(), GenesisMaxBlacklistedRounds)

	next := bl.Next(4, GenesisMaxBlacklistedRounds, weights, map[uint16]uint64{2: 4}, nil)
	require.NoError(t, bl.VerifyProposedBlacklist(next, 4, weights))

	otherWeights := []uint64{1, 10, 1, 1}
	err := bl.VerifyProposedBlacklist(next, 4, otherWeights)
	require.ErrorContains(t, err, errBlacklistInvalidBlacklist.Error())
}

func TestBlacklistCapIsWeightBased(t *testing.T) {
	t.Run("equal weights allow up to f blacklisted nodes", func(t *testing.T) {
		const nodeCount = 10
		weights := equalWeights(nodeCount)
		bl := Blacklist{nodeCount: nodeCount, totalWeight: nodeCount, maxBlacklistedOrbits: 1}
		threshold := bl.computeThreshold()

		bl.suspectedNodes = suspectedNodes{
			{NodeIndex: 0, SuspectingWeight: threshold, OrbitSuspected: 1},
			{NodeIndex: 1, SuspectingWeight: threshold, OrbitSuspected: 1},
		}
		bl.setNodeSuspected(1, 2, weights[9], weights)
		require.Len(t, bl.suspectedNodes, 3, "a third node can still be accused")
		bl.suspectedNodes[2].SuspectingWeight = threshold

		bl.setNodeSuspected(1, 3, weights[9], weights)
		require.Len(t, bl.suspectedNodes, 3, "f=3 nodes are blacklisted, a fourth cannot be accused")
	})

	t.Run("a heavy node cannot be blacklisted", func(t *testing.T) {
		// Node 0 holds half of the weight, more than the threshold of 4 out of 10.
		weights := []uint64{5, 1, 1, 1, 1, 1}
		bl := Blacklist{nodeCount: 6, totalWeight: uint64s(weights).Sum(), maxBlacklistedOrbits: 1}

		bl.setNodeSuspected(1, 0, weights[1], weights)
		require.Empty(t, bl.suspectedNodes)

		bl.setNodeSuspected(1, 2, weights[1], weights)
		require.Len(t, bl.suspectedNodes, 1, "a light node can still be accused")
	})

	t.Run("the weight of the accused node counts towards the cap", func(t *testing.T) {
		// The threshold is 4 out of 10, and node 1 (weight 1) is already blacklisted.
		weights := []uint64{3, 1, 1, 1, 1, 1, 1, 1}
		bl := Blacklist{nodeCount: 8, totalWeight: uint64s(weights).Sum(), maxBlacklistedOrbits: 1}
		bl.suspectedNodes = suspectedNodes{{NodeIndex: 1, SuspectingWeight: 4, OrbitSuspected: 1}}
		require.True(t, bl.IsNodeSuspected(1))
		require.Len(t, bl.suspectedNodes, 1)

		// Blacklisting node 0 (weight 3) would put a threshold of the weight on the blacklist.
		bl.setNodeSuspected(1, 0, weights[3], weights)
		require.Len(t, bl.suspectedNodes, 1)
		require.False(t, bl.IsNodeSuspected(0))

		// Node 2 (weight 1) can still be accused.
		bl.setNodeSuspected(1, 2, weights[3], weights)
		bl.setNodeSuspected(1, 2, weights[4], weights)
		bl.setNodeSuspected(1, 2, weights[5], weights)
		bl.setNodeSuspected(1, 2, weights[6], weights)
		require.Equal(t, suspectedNodes{
			{NodeIndex: 1, SuspectingWeight: 4, OrbitSuspected: 1},
			{NodeIndex: 2, SuspectingWeight: 4, OrbitSuspected: 1},
		}, bl.suspectedNodes)
		require.True(t, bl.IsNodeSuspected(1))
		require.True(t, bl.IsNodeSuspected(2))
	})

	t.Run("only blacklisted nodes count towards the cap", func(t *testing.T) {
		// Node 1 is accused but not blacklisted, so its weight does not count towards the cap.
		weights := []uint64{3, 1, 1, 1, 1, 1, 1, 1}
		bl := Blacklist{nodeCount: 8, totalWeight: uint64s(weights).Sum(), maxBlacklistedOrbits: 1}
		bl.suspectedNodes = suspectedNodes{{NodeIndex: 1, SuspectingWeight: 3, OrbitSuspected: 1}}
		require.False(t, bl.IsNodeSuspected(1))
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
				nodeCount:            nodeCount,
				totalWeight:          nodeCount,
				maxBlacklistedOrbits: 10,
				suspectedNodes:       testCase.suspectedNodes,
			}
			next := bl.applyUpdates(testCase.updates, testCase.round, testCase.weights)
			require.Equal(t, testCase.expectedSuspectedNodes, next.suspectedNodes)
		})
	}
}

func TestBlacklistTotalWeightIsPreserved(t *testing.T) {
	weights := []uint64{10, 1, 1, 1}
	bl := NewBlacklist(4, 13, GenesisMaxBlacklistedRounds)
	require.Equal(t, uint64(13), bl.totalWeight)

	next := bl.Next(4, GenesisMaxBlacklistedRounds, weights, map[uint16]uint64{2: 4}, nil)
	require.Equal(t, uint64(13), next.totalWeight, "Next should carry over the total weight")

	cloned := next.Clone()
	require.Equal(t, uint64(13), cloned.totalWeight, "Clone should carry over the total weight")

	var decoded Blacklist
	require.NoError(t, decoded.FromBytes(next.Bytes()))
	require.Equal(t, uint64(13), decoded.totalWeight, "serialization should preserve the total weight")
	require.True(t, next.Equals(&decoded))

	// A blacklist that arrives over the wire is verified against its parent with the total weight intact.
	require.NoError(t, bl.VerifyProposedBlacklist(decoded, 4, weights))

	stateless := next.Stateless(5, 20)
	require.Equal(t, uint64(20), stateless.totalWeight, "Stateless should take the total weight of the new validator set")
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
