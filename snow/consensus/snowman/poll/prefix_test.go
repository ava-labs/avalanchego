// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func nameVertices(pg *prefixGroup) map[*prefixGroup]string {
	vertexNames := make(map[*prefixGroup]string)
	nextVertexName := 'a'
	pg.traverse(func(pg *prefixGroup) {
		vertexNames[pg] = string(nextVertexName)
		nextVertexName++
	})
	return vertexNames
}

func TestSharedPrefixes(t *testing.T) {
	for _, tst := range []struct {
		name            string
		input           []ids.ID
		expectedEdges   string
		expectedMembers map[string][]ids.ID
	}{
		{
			name:          "no shared prefix",
			input:         []ids.ID{{0xff, 0x0f}, {0x00, 0x1f}},
			expectedEdges: `(a,b)(a,c)`,
			expectedMembers: map[string][]ids.ID{
				"a": {{0xff, 0x0f}, {0x00, 0x1f}},
				"b": {{0x00, 0x1f}},
				"c": {{0xff, 0x0f}},
			},
		},
		{
			name:          "shared prefix for simple pair",
			input:         []ids.ID{blkID2, blkID4},
			expectedEdges: `(a,b)(a,c)`,
			expectedMembers: map[string][]ids.ID{
				"a": {blkID2, blkID4},
				"b": {blkID4},
				"c": {blkID2},
			},
		},
		{
			name:          "shared prefix for pair",
			input:         []ids.ID{{0xf0, 0x0f}, {0xf0, 0x1f}},
			expectedEdges: `(a,b)(a,c)`,
			expectedMembers: map[string][]ids.ID{
				"a": {{0xf0, 0x0f}, {0xf0, 0x1f}},
				"b": {{0xf0, 0x0f}},
				"c": {{0xf0, 0x1f}},
			},
		},
		{
			name:          "shared prefix pair out of three descendants",
			input:         []ids.ID{{0xf0, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}},
			expectedEdges: `(a,b)(a,c)(c,d)(c,e)`,
			expectedMembers: map[string][]ids.ID{
				"a": {{0xf0, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}},
				"b": {{0xf0, 0xff}},
				"c": {{0xff, 0xf0}, {0x0f, 0xff}},
				"d": {{0x0f, 0xff}},
				"e": {{0xff, 0xf0}},
			},
		},
		{
			name:          "shared prefix for quad",
			input:         []ids.ID{{0xff, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}, {0x0f, 0xf0}},
			expectedEdges: `(a,b)(a,e)(b,c)(b,d)(e,f)(e,g)`,
			expectedMembers: map[string][]ids.ID{
				"a": {{0xff, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}, {0x0f, 0xf0}},
				"e": {{0xff, 0xff}, {0xff, 0xf0}},
				"b": {{0x0f, 0xff}, {0x0f, 0xf0}},
				"d": {{0x0f, 0xff}},
				"c": {{0x0f, 0xf0}},
				"f": {{0xff, 0xf0}},
				"g": {{0xff, 0xff}},
			},
		},
		{
			name:          "same string included twice",
			input:         []ids.ID{{0xff, 0x0f}, {0x00, 0x1f}, {0xff, 0x0f}, {0x00, 0x1f}},
			expectedEdges: `(a,b)(a,c)`,
			expectedMembers: map[string][]ids.ID{
				"a": {{0xff, 0x0f}, {0x00, 0x1f}},
				"b": {{0x00, 0x1f}},
				"c": {{0xff, 0x0f}},
			},
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			pg := longestSharedPrefixes(tst.input)
			vertexNames := nameVertices(pg)

			edges := bytes.Buffer{}
			members := make(map[string][]ids.ID)

			pg.traverse(func(pg *prefixGroup) {
				pgVertexName := vertexNames[pg]
				members[pgVertexName] = pg.members
				if pg.children[0] != nil {
					fmt.Fprintf(&edges, "(%s,%s)", pgVertexName, vertexNames[pg.children[0]])
				}
				if pg.children[1] != nil {
					fmt.Fprintf(&edges, "(%s,%s)", pgVertexName, vertexNames[pg.children[1]])
				}
			})

			require.Equal(t, tst.expectedEdges, edges.String())
			require.Equal(t, tst.expectedMembers, members)
		})
	}
}

func TestBifurcationsWithCommonPrefix(t *testing.T) {
	pg := &prefixGroup{
		members: []ids.ID{{0, 1, 1}, {0, 0, 1}, {0, 1, 0}, {0, 0, 0}},
		children: [2]*prefixGroup{
			{
				index:   1,
				members: []ids.ID{{0, 1, 0}, {0, 0, 0}},
				children: [2]*prefixGroup{
					{
						index:   3,
						members: []ids.ID{{0, 0, 0}},
					},
					{
						index:   3,
						members: []ids.ID{{0, 1, 0}},
					},
				},
			},
			{
				index:   1,
				members: []ids.ID{{0, 1, 1}, {0, 0, 1}},
				children: [2]*prefixGroup{
					{
						index:   3,
						members: []ids.ID{{0, 1, 1}},
					},
					{
						index:   3,
						members: []ids.ID{{0, 0, 1}},
					},
				},
			},
		},
	}

	expectedTraversalOrder := [][]ids.ID{
		{{0, 1, 0}, {0, 0, 0}},
		{{0, 1, 1}, {0, 0, 1}},
	}

	actualOrder := make([][]ids.ID, 0, 2)

	pg.bifurcationsWithCommonPrefix(func(actual []ids.ID) {
		actualOrder = append(actualOrder, actual)
	})

	require.Equal(t, expectedTraversalOrder, actualOrder)
}

func TestDeduplicate(t *testing.T) {
	actual := deduplicate([]ids.ID{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}, {10, 11, 12}, {1, 2, 3}, {13, 14, 15}, {4, 5, 6}, {10, 11, 12}, {13, 14, 15}})
	require.Equal(t, []ids.ID{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}, {10, 11, 12}, {13, 14, 15}}, actual)
}
