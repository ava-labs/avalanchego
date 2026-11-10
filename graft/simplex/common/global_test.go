// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestNodeIDs(t *testing.T) {
	nodeIDs := NodeIDs{
		{ShortID: ids.ShortID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}},
	}

	for i := range nodeIDs {
		require.NotContains(t, nodeIDs, nodeIDs.Remove(nodeIDs[i]))
		for j := range nodeIDs {
			if i == j {
				continue
			}
			require.Contains(t, nodeIDs, nodeIDs[j])
		}
	}
}

// TestNodesEqual checks Equal ignores order but compares
// length, Id, PK and Weight of every node.
func TestNodesEqual(t *testing.T) {
	a := Node{Id: NodeID{ShortID: ids.ShortID{1}}, Weight: 10, PK: PublicKeyBytes{0xa}}
	b := Node{Id: NodeID{ShortID: ids.ShortID{2}}, Weight: 20, PK: PublicKeyBytes{0xb}}
	c := Node{Id: NodeID{ShortID: ids.ShortID{3}}, Weight: 30, PK: PublicKeyBytes{0xc}}

	testCases := []struct {
		name  string
		nws   Nodes
		other Nodes
		equal bool
	}{
		{name: "both nil", equal: true},
		{name: "nil and empty", other: Nodes{}, equal: true},
		{name: "same order", nws: Nodes{a, b, c}, other: Nodes{a, b, c}, equal: true},
		{name: "different order", nws: Nodes{a, b, c}, other: Nodes{c, a, b}, equal: true},
		{name: "different length", nws: Nodes{a, b}, other: Nodes{a, b, c}, equal: false},
		{name: "empty and non empty", nws: Nodes{}, other: Nodes{a}, equal: false},
		{name: "different id", nws: Nodes{a, b}, other: Nodes{a, {Id: NodeID{ShortID: ids.ShortID{9}}, Weight: b.Weight, PK: b.PK}}, equal: false},
		{name: "different weight", nws: Nodes{a, b}, other: Nodes{a, {Id: b.Id, Weight: 99, PK: b.PK}}, equal: false},
		{name: "different pk", nws: Nodes{a, b}, other: Nodes{a, {Id: b.Id, Weight: b.Weight, PK: PublicKeyBytes{0xff}}}, equal: false},
		{name: "duplicate vs distinct", nws: Nodes{a, a}, other: Nodes{a, b}, equal: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.equal, testCase.nws.Equal(testCase.other))
			require.Equal(t, testCase.equal, testCase.other.Equal(testCase.nws))
		})
	}
}

// TestNodesEqualDoesNotMutate checks Equal sorts clones and
// leaves the receiver and argument in their original order.
func TestNodesEqualDoesNotMutate(t *testing.T) {
	a := Node{Id: NodeID{ShortID: ids.ShortID{1}}, Weight: 10, PK: PublicKeyBytes{0xa}}
	b := Node{Id: NodeID{ShortID: ids.ShortID{2}}, Weight: 20, PK: PublicKeyBytes{0xb}}

	nws := Nodes{b, a}
	other := Nodes{a, b}
	require.True(t, nws.Equal(other))
	require.Equal(t, Nodes{b, a}, nws)
	require.Equal(t, Nodes{a, b}, other)
}

// TestNodesWeights checks Weights returns the weight of every node,
// in the same order as the nodes.
func TestNodesWeights(t *testing.T) {
	a := Node{Id: NodeID{ShortID: ids.ShortID{1}}, Weight: 10, PK: PublicKeyBytes{0xa}}
	b := Node{Id: NodeID{ShortID: ids.ShortID{2}}, Weight: 20, PK: PublicKeyBytes{0xb}}
	c := Node{Id: NodeID{ShortID: ids.ShortID{3}}, Weight: 30, PK: PublicKeyBytes{0xc}}

	testCases := []struct {
		name     string
		nws      Nodes
		expected uint64s
	}{
		{name: "nil", expected: uint64s{}},
		{name: "empty", nws: Nodes{}, expected: uint64s{}},
		{name: "single node", nws: Nodes{a}, expected: uint64s{10}},
		{name: "order is preserved", nws: Nodes{c, a, b}, expected: uint64s{30, 10, 20}},
		{name: "zero weight", nws: Nodes{a, {Id: NodeID{ShortID: ids.ShortID{4}}}}, expected: uint64s{10, 0}},
		{name: "equal weights", nws: NodeIDs{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}}.EqualWeightedNodes(), expected: uint64s{1, 1, 1}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, testCase.nws.Weights())
		})
	}
}

// TestNodesWeightsDoesNotAlias checks that modifying the returned weights
// does not modify the nodes.
func TestNodesWeightsDoesNotAlias(t *testing.T) {
	nws := Nodes{{Id: NodeID{ShortID: ids.ShortID{1}}, Weight: 10}, {Id: NodeID{ShortID: ids.ShortID{2}}, Weight: 20}}

	weights := nws.Weights()
	weights[0] = 99

	require.Equal(t, uint64(10), nws[0].Weight)
}

func TestUint64sSum(t *testing.T) {
	testCases := []struct {
		name     string
		ns       uint64s
		expected uint64
	}{
		{name: "nil", expected: 0},
		{name: "empty", ns: uint64s{}, expected: 0},
		{name: "single element", ns: uint64s{7}, expected: 7},
		{name: "several elements", ns: uint64s{10, 20, 30}, expected: 60},
		{name: "zeros", ns: uint64s{0, 0, 0}, expected: 0},
		{name: "max value", ns: uint64s{math.MaxUint64}, expected: math.MaxUint64},
		{name: "adds up to max value", ns: uint64s{math.MaxUint64 - 5, 5}, expected: math.MaxUint64},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, testCase.ns.Sum())
		})
	}
}

func TestNodesWeightsSum(t *testing.T) {
	nws := Nodes{{Id: NodeID{ShortID: ids.ShortID{1}}, Weight: 10}, {Id: NodeID{ShortID: ids.ShortID{2}}, Weight: 20}, {Id: NodeID{ShortID: ids.ShortID{3}}, Weight: 30}}
	require.Equal(t, uint64(60), nws.Weights().Sum())
}

// TestNodeIDCompare checks Compare for node IDs that set only one of ShortID
// and LongID. The comparison looks at LongID first, so an ID that sets only
// ShortID sorts before one that sets only LongID.
func TestNodeIDCompare(t *testing.T) {
	short := func(b byte) NodeID { return NodeID{ShortID: ids.ShortID{19: b}} }
	long := func(b byte) NodeID { return NodeID{LongID: [32]byte{31: b}} }
	// These set the first byte, to check that the comparison isn't limited to the last one.
	shortFirstByte := NodeID{ShortID: ids.ShortID{0: 1}}
	longFirstByte := NodeID{LongID: [32]byte{0: 1}}

	testCases := []struct {
		name     string
		a, b     NodeID
		expected int
	}{
		{name: "both empty", a: NodeID{}, b: NodeID{}, expected: 0},
		{name: "empty and short", a: NodeID{}, b: short(1), expected: -1},
		{name: "short and empty", a: short(1), b: NodeID{}, expected: 1},
		{name: "empty and long", a: NodeID{}, b: long(1), expected: -1},
		{name: "long and empty", a: long(1), b: NodeID{}, expected: 1},

		{name: "equal short", a: short(5), b: short(5), expected: 0},
		{name: "smaller short", a: short(4), b: short(5), expected: -1},
		{name: "larger short", a: short(6), b: short(5), expected: 1},
		{name: "short differs in first byte", a: shortFirstByte, b: short(255), expected: 1},

		{name: "equal long", a: long(5), b: long(5), expected: 0},
		{name: "smaller long", a: long(4), b: long(5), expected: -1},
		{name: "larger long", a: long(6), b: long(5), expected: 1},
		{name: "long differs in first byte", a: longFirstByte, b: long(255), expected: 1},

		{name: "short before long", a: short(255), b: long(1), expected: -1},
		{name: "long after short", a: long(1), b: short(255), expected: 1},
		{name: "short first byte before long", a: shortFirstByte, b: long(1), expected: -1},
		{name: "long first byte after short", a: longFirstByte, b: short(255), expected: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, testCase.a.Compare(testCase.b))
			require.Equal(t, -testCase.expected, testCase.b.Compare(testCase.a))
		})
	}
}

func TestNodeIDAvalancheIDConversion(t *testing.T) {
	avaID := ids.GenerateTestNodeID()

	nodeID := NodeIDFromAvalancheID(avaID)
	require.Equal(t, ids.ShortID(avaID), nodeID.ShortID)
	require.Equal(t, [32]byte{}, nodeID.LongID)

	roundTripped, err := nodeID.ToAvalancheID()
	require.NoError(t, err)
	require.Equal(t, avaID, roundTripped)

	nodeID.LongID[0] = 1
	_, err = nodeID.ToAvalancheID()
	require.ErrorIs(t, err, errLongNodeID)
}
