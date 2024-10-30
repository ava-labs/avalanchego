// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
		name                        string
		input                       []ids.ID
		expectedEdges               string
		expectedMembers             map[string][]ids.ID
		expectedCommonPrefixes      [][]uint8
		expectedCommonPrefixMembers [][]ids.ID
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
			expectedCommonPrefixes:      [][]uint8{},
			expectedCommonPrefixMembers: [][]ids.ID{},
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
			expectedCommonPrefixes:      [][]uint8{{0x0}},
			expectedCommonPrefixMembers: [][]ids.ID{{blkID2, blkID4}},
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
			expectedCommonPrefixes:      [][]uint8{{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1}},
			expectedCommonPrefixMembers: [][]ids.ID{{{0xf0, 0x0f}, {0xf0, 0x1f}}},
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
			expectedCommonPrefixes:      [][]uint8{{1, 1, 1, 1}},
			expectedCommonPrefixMembers: [][]ids.ID{{{0xff, 0xf0}, {0x0f, 0xff}}},
		},
		{
			name:          "shared prefix for quad",
			input:         []ids.ID{{0xff, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}, {0x0f, 0xf0}},
			expectedEdges: `(a,b)(a,e)(b,c)(b,d)(e,f)(e,g)`,
			expectedCommonPrefixMembers: [][]ids.ID{
				{{0xff, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}, {0x0f, 0xf0}},
				{{0x0f, 0xff}, {0x0f, 0xf0}},
				{{0xff, 0xff}, {0xff, 0xf0}},
			},
			expectedMembers: map[string][]ids.ID{
				"a": {{0xff, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}, {0x0f, 0xf0}},
				"e": {{0xff, 0xff}, {0xff, 0xf0}},
				"b": {{0x0f, 0xff}, {0x0f, 0xf0}},
				"d": {{0x0f, 0xff}},
				"c": {{0x0f, 0xf0}},
				"f": {{0xff, 0xf0}},
				"g": {{0xff, 0xff}},
			},
			expectedCommonPrefixes: [][]uint8{
				{1, 1, 1, 1}, {1, 1, 1, 1, 0, 0, 0, 0}, {1, 1, 1, 1, 1, 1, 1, 1},
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
				if pg.zg != nil {
					fmt.Fprintf(&edges, "(%s,%s)", pgVertexName, vertexNames[pg.zg])
				}
				if pg.og != nil {
					fmt.Fprintf(&edges, "(%s,%s)", pgVertexName, vertexNames[pg.og])
				}
			})

			// commonPrefixMembers holds common prefixes of prefix groups as bifurcationsWithCommonPrefix()
			// traverses starting from the top prefix group.
			// It does not contain leaf nodes, as bifurcationsWithCommonPrefix() does not visit leaf nodes in its traversal.

			// commonPrefixMembers contains the IDs of the prefix group that corresponds to the commonPrefixes.
			// The intention of having it is showing how each bifurcation splits the members according to their common prefix.
			// Had bifurcationsWithCommonPrefix() visited leaf prefix groups, the commonPrefixMembers would shrink down to a single ID.
			// Since bifurcationsWithCommonPrefix() doesn't visit leaf prefix groups, commonPrefixMembers can contain sets of sets as small as two elements.

			commonPrefixMembers := make([][]ids.ID, 0, len(tst.expectedCommonPrefixMembers))
			commonPrefixes := make([][]uint8, 0, len(tst.expectedCommonPrefixes))

			pg.bifurcationsWithCommonPrefix(func(members []ids.ID, prefix []uint8) {
				commonPrefixMembers = append(commonPrefixMembers, members)
				commonPrefixes = append(commonPrefixes, prefix)
			})

			require.Equal(t, tst.expectedEdges, edges.String())
			require.Equal(t, tst.expectedMembers, members)
			require.Equal(t, tst.expectedCommonPrefixes, commonPrefixes)
			require.Equal(t, tst.expectedCommonPrefixMembers, commonPrefixMembers)
		})
	}
}
