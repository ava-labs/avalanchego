// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeIDs(t *testing.T) {
	nodeIDs := NodeIDs{
		{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
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
	a := Node{Id: NodeID{1}, Weight: 10, PK: PublicKeyBytes{0xa}}
	b := Node{Id: NodeID{2}, Weight: 20, PK: PublicKeyBytes{0xb}}
	c := Node{Id: NodeID{3}, Weight: 30, PK: PublicKeyBytes{0xc}}

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
		{name: "different id", nws: Nodes{a, b}, other: Nodes{a, {Id: NodeID{9}, Weight: b.Weight, PK: b.PK}}, equal: false},
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
	a := Node{Id: NodeID{1}, Weight: 10, PK: PublicKeyBytes{0xa}}
	b := Node{Id: NodeID{2}, Weight: 20, PK: PublicKeyBytes{0xb}}

	nws := Nodes{b, a}
	other := Nodes{a, b}
	require.True(t, nws.Equal(other))
	require.Equal(t, Nodes{b, a}, nws)
	require.Equal(t, Nodes{a, b}, other)
}

// TestNodesWeights checks Weights returns the weight of every node,
// in the same order as the nodes.
func TestNodesWeights(t *testing.T) {
	a := Node{Id: NodeID{1}, Weight: 10, PK: PublicKeyBytes{0xa}}
	b := Node{Id: NodeID{2}, Weight: 20, PK: PublicKeyBytes{0xb}}
	c := Node{Id: NodeID{3}, Weight: 30, PK: PublicKeyBytes{0xc}}

	testCases := []struct {
		name     string
		nws      Nodes
		expected uint64s
	}{
		{name: "nil", expected: uint64s{}},
		{name: "empty", nws: Nodes{}, expected: uint64s{}},
		{name: "single node", nws: Nodes{a}, expected: uint64s{10}},
		{name: "order is preserved", nws: Nodes{c, a, b}, expected: uint64s{30, 10, 20}},
		{name: "zero weight", nws: Nodes{a, {Id: NodeID{4}}}, expected: uint64s{10, 0}},
		{name: "equal weights", nws: NodeIDs{{1}, {2}, {3}}.EqualWeightedNodes(), expected: uint64s{1, 1, 1}},
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
	nws := Nodes{{Id: NodeID{1}, Weight: 10}, {Id: NodeID{2}, Weight: 20}}

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
	nws := Nodes{{Id: NodeID{1}, Weight: 10}, {Id: NodeID{2}, Weight: 20}, {Id: NodeID{3}, Weight: 30}}
	require.Equal(t, uint64(60), nws.Weights().Sum())
}
