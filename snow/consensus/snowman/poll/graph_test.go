// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

func TestBuildVotesGraph(t *testing.T) {
	g := &voteVertex{
		id: ids.ID{1},
		descendants: []*voteVertex{
			{id: ids.ID{11}, descendants: []*voteVertex{
				{id: ids.ID{111}},
				{id: ids.ID{112}},
				{id: ids.ID{113}},
			}},
			{id: ids.ID{12}},
			{id: ids.ID{13}},
		},
	}
	wireParents(g)

	getParent := getParentFunc(g)

	var votes bag.Bag[ids.ID]
	g.traverse(func(v *voteVertex) {
		votes.Add(v.id)
	})

	parents := make(map[ids.ID]ids.ID)
	children := make(map[ids.ID]*bag.Bag[ids.ID])

	votesGraph := buildVoteGraph(getParent, votes)
	votesGraph.traverse(func(v *voteVertex) {
		if v.parent == nil {
			parents[v.id] = ids.Empty
		} else {
			parents[v.id] = v.parent.id
		}

		if len(v.descendants) == 0 {
			return
		}

		var childrenIDs bag.Bag[ids.ID]
		for _, child := range v.descendants {
			childrenIDs.Add(child.id)
		}
		children[v.id] = &childrenIDs
	})

	require.Equal(t, map[ids.ID]ids.ID{
		{1}:   ids.Empty,
		{11}:  {1},
		{12}:  {1},
		{13}:  {1},
		{111}: {11},
		{112}: {11},
		{113}: {11},
	}, parents)

	expected1 := bag.Of(ids.ID{11}, ids.ID{12}, ids.ID{13})
	expected11 := bag.Of(ids.ID{111}, ids.ID{112}, ids.ID{113})

	require.Len(t, children, 2)
	require.True(t, children[ids.ID{1}].Equals(expected1))
	require.True(t, children[ids.ID{11}].Equals(expected11))
}

func getParentFunc(g *voteVertex) func(id ids.ID) (ids.ID, bool) {
	return func(id ids.ID) (ids.ID, bool) {
		var result ids.ID
		g.traverse(func(v *voteVertex) {
			if v.id.Compare(id) == 0 {
				if v.parent == nil {
					result = ids.Empty
				} else {
					result = v.parent.id
				}
			}
		})
		return result, result != ids.Empty
	}
}

func TestComputeTransitiveVoteCountGraph(t *testing.T) {
	g := &voteVertex{
		id: ids.ID{1},
		descendants: []*voteVertex{
			{id: ids.ID{11}, descendants: []*voteVertex{
				{id: ids.ID{111}},
				{id: ids.ID{112}},
				{id: ids.ID{113}},
			}},
			{id: ids.ID{12}},
			{id: ids.ID{13}},
		},
	}
	wireParents(g)
	var votes bag.Bag[ids.ID]
	g.traverse(func(v *voteVertex) {
		votes.Add(v.id)
	})

	getParent := getParentFunc(g)
	votesGraph := buildVoteGraph(getParent, votes)
	transitiveVotes := computeTransitiveVoteCountGraph(&votesGraph, votes)

	expected := len(transitiveVotes.List())
	actual := votes.Len()

	require.Equal(t, expected, actual)

	for id, expectedVotes := range map[ids.ID]int{
		{12}:  1,
		{13}:  1,
		{111}: 1,
		{112}: 1,
		{113}: 1,
		{11}:  4,
		{1}:   7,
	} {
		require.Equal(t, expectedVotes, transitiveVotes.Count(id))
	}
}

func TestTopologicalSortTraversal(t *testing.T) {
	v1 := &voteVertex{id: ids.ID{1}}
	v2 := &voteVertex{id: ids.ID{2}}
	v3 := &voteVertex{id: ids.ID{3}}
	v4 := &voteVertex{id: ids.ID{4}}
	v5 := &voteVertex{id: ids.ID{5}}
	v6 := &voteVertex{id: ids.ID{6}}
	v7 := &voteVertex{id: ids.ID{7}}
	v8 := &voteVertex{id: ids.ID{8}}

	//
	//    v7        v8
	//  v3  v5    v4  v6
	// v1        v2

	v1.parent = v3
	v2.parent = v4
	v3.parent = v7
	v4.parent = v8
	v5.parent = v7
	v6.parent = v8

	v3.descendants = []*voteVertex{v1}
	v4.descendants = []*voteVertex{v2}
	v7.descendants = []*voteVertex{v3, v5}
	v8.descendants = []*voteVertex{v4, v6}

	vg := voteGraph{leaves: []*voteVertex{v1, v2, v5, v6}, roots: []*voteVertex{v7, v8}, vertexCount: 8}

	order := make(map[ids.ID]int)
	var currentOrder int

	vg.topologicalSortTraversal(func(v *voteVertex) {
		order[v.id] = currentOrder
		currentOrder++
	})

	require.Less(t, order[v1.id], order[v3.id])
	require.Less(t, order[v3.id], order[v7.id])
	require.Less(t, order[v5.id], order[v7.id])
	require.Less(t, order[v2.id], order[v4.id])
	require.Less(t, order[v4.id], order[v8.id])
	require.Less(t, order[v6.id], order[v8.id])
}

func wireParents(v *voteVertex) {
	v.traverse(func(vertex *voteVertex) {
		for _, child := range vertex.descendants {
			child.parent = vertex
		}
	})
}
