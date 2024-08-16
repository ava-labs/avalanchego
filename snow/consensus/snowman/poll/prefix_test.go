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

func nameVertices(pg *prefixGroup) {
	nextVertexName := 'a'
	pg.traverse(func(pg *prefixGroup) {
		pg.vertexName = nextVertexName
		nextVertexName++
	})
}

func TestSharedPrefixes(t *testing.T) {
	for _, tst := range []struct {
		name                                        string
		input                                       []ids.ID
		expectedEdges                               string
		expectedMembers                             map[string][]ids.ID
		expectedBifurcationsWithCommonPrefix        [][]uint8
		expectedBifurcationsWithCommonPrefixMembers [][]ids.ID
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
			expectedBifurcationsWithCommonPrefix:        [][]uint8{},
			expectedBifurcationsWithCommonPrefixMembers: [][]ids.ID{},
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
			expectedBifurcationsWithCommonPrefix:        [][]uint8{{0x0}},
			expectedBifurcationsWithCommonPrefixMembers: [][]ids.ID{{blkID2, blkID4}},
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
			expectedBifurcationsWithCommonPrefix:        [][]uint8{{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1}},
			expectedBifurcationsWithCommonPrefixMembers: [][]ids.ID{{{0xf0, 0x0f}, {0xf0, 0x1f}}},
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
			expectedBifurcationsWithCommonPrefix:        [][]uint8{{1, 1, 1, 1}},
			expectedBifurcationsWithCommonPrefixMembers: [][]ids.ID{{{0xff, 0xf0}, {0x0f, 0xff}}},
		},
		{
			name:          "shared prefix for quad",
			input:         []ids.ID{{0xff, 0xff}, {0xff, 0xf0}, {0x0f, 0xff}, {0x0f, 0xf0}},
			expectedEdges: `(a,b)(a,e)(b,c)(b,d)(e,f)(e,g)`,
			expectedBifurcationsWithCommonPrefixMembers: [][]ids.ID{
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
			expectedBifurcationsWithCommonPrefix: [][]uint8{
				{1, 1, 1, 1}, {1, 1, 1, 1, 0, 0, 0, 0}, {1, 1, 1, 1, 1, 1, 1, 1},
			},
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			pg := longestSharedPrefixes(tst.input)
			nameVertices(pg)

			edges := bytes.Buffer{}
			members := make(map[string][]ids.ID)

			pg.traverse(func(pg *prefixGroup) {
				members[string(pg.vertexName)] = pg.members
				if pg.zg != nil {
					fmt.Fprintf(&edges, "(%s,%s)", string(pg.vertexName), string(pg.zg.vertexName))
				}
				if pg.og != nil {
					fmt.Fprintf(&edges, "(%s,%s)", string(pg.vertexName), string(pg.og.vertexName))
				}
			})

			bifurcationsWithCommonPrefixMembers := make([][]ids.ID, 0, len(tst.expectedBifurcationsWithCommonPrefixMembers))
			bifurcationsWithCommonPrefix := make([][]uint8, 0, len(tst.expectedBifurcationsWithCommonPrefix))

			pg.bifurcationsWithCommonPrefix(func(members []ids.ID, prefix []uint8) {
				bifurcationsWithCommonPrefixMembers = append(bifurcationsWithCommonPrefixMembers, members)
				bifurcationsWithCommonPrefix = append(bifurcationsWithCommonPrefix, prefix)
			})

			require.Equal(t, tst.expectedEdges, edges.String())
			require.Equal(t, tst.expectedMembers, members)
			require.Equal(t, tst.expectedBifurcationsWithCommonPrefix, bifurcationsWithCommonPrefix)
			require.Equal(t, tst.expectedBifurcationsWithCommonPrefixMembers, bifurcationsWithCommonPrefixMembers)
		})
	}
}
