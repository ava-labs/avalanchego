// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

// voteVertex encapsulates an ID and points to its parent and descendants.
type voteVertex struct {
	id          ids.ID
	parent      *voteVertex
	descendants []*voteVertex
}

// traverse invokes f() on this voteVertex and all its descendants in pre-order traversal.
func (v *voteVertex) traverse(f func(*voteVertex)) {
	f(v)
	for _, u := range v.descendants {
		u.traverse(f)
	}
}

// voteGraph is a collection of vote vertices.
// it must be initialized via buildVoteGraph() and not explicitly.
type voteGraph struct {
	vertexCount int
	leaves      []*voteVertex
	roots       []*voteVertex
}

// traverse traverses over all vertices in the voteGraph in pre-order traversal.
func (vg *voteGraph) traverse(f func(*voteVertex)) {
	for _, root := range vg.roots {
		root.traverse(f)
	}
}

// topologicalSortTraversal invokes f() on all vote vertices in the voteGraph according to
// the topological order of the vertices.
func (vg *voteGraph) topologicalSortTraversal(f func(*voteVertex)) {
	// We hold a counter for each vertex
	childCount := make(map[ids.ID]int, vg.vertexCount)
	vg.traverse(func(v *voteVertex) {
		childCount[v.id] = len(v.descendants)
	})

	leaves := make([]*voteVertex, len(vg.leaves))
	copy(leaves, vg.leaves)

	// Iterate from leaves to roots and recursively apply f() over each vertex
	for len(leaves) > 0 {
		newLeaves := make([]*voteVertex, 0, len(leaves))
		for _, leaf := range leaves {
			f(leaf)
			if leaf.parent == nil {
				continue
			}
			childCount[leaf.parent.id]--
			if childCount[leaf.parent.id] == 0 {
				newLeaves = append(newLeaves, leaf.parent)
			}
		}
		leaves = newLeaves
	}
}

func clone(votes bag.Bag[ids.ID]) bag.Bag[ids.ID] {
	var votesClone bag.Bag[ids.ID]
	for _, id := range votes.List() {
		votesClone.AddCount(id, votes.Count(id))
	}
	return votesClone
}

func findLeaves(idToVertex map[ids.ID]*voteVertex) []*voteVertex {
	leaves := make([]*voteVertex, 0, len(idToVertex))
	for _, v := range idToVertex {
		if len(v.descendants) == 0 {
			leaves = append(leaves, v)
		}
	}
	return leaves
}

// buildVoteGraph receives as input a function that returns the ID of a block, or Empty if unknown,
// as well as a bag of IDs (The bag is for enforcing uniqueness among the IDs in contrast to a list).
// It returns a voteGraph where each vertex corresponds to an ID and is linked to vertices
// according to what getParentFunc() returns for each ID.
func buildVoteGraph(getParent func(ids.ID) ids.ID, votes bag.Bag[ids.ID]) voteGraph {
	idList := votes.List()

	id2Vertex := make(map[ids.ID]*voteVertex, len(idList))
	roots := make([]*voteVertex, 0, len(idList))

	// Build a graph out of the vertices that correspond to the IDs of the votes.
	for _, id := range idList {
		_, ok := id2Vertex[id]
		if ok || id == ids.Empty {
			continue
		}
		v := &voteVertex{id: id, descendants: make([]*voteVertex, 0, 2)}
		id2Vertex[id] = v
	}

	// Add the parents of the IDs to the graph, for those that are not already there.
	for _, id := range idList {
		parent := getParent(id)
		// If this parent isn't found, it must be already finalized, so don't add it to the graph.
		if parent == ids.Empty {
			continue
		}
		_, ok := id2Vertex[parent]
		// If the parent is not finalized we can vote on it, so add it to the graph
		if !ok {
			v := &voteVertex{id: parent, descendants: make([]*voteVertex, 0, 2)}
			id2Vertex[parent] = v
		}
	}

	for id, v := range id2Vertex {
		parent := getParent(id)

		u, ok := id2Vertex[parent]
		if ok {
			v.parent = u
			u.descendants = append(u.descendants, v)
		} else {
			roots = append(roots, v)
		}
	}

	leaves := findLeaves(id2Vertex)

	return voteGraph{leaves: leaves, roots: roots, vertexCount: len(id2Vertex)}
}

// computeTransitiveVoteCountGraph receives a vote graph and corresponding votes for each vertex ID.
// Returns a new bag where element represents the number of votes for transitive descendents in the graph.
func computeTransitiveVoteCountGraph(graph *voteGraph, votes bag.Bag[ids.ID]) bag.Bag[ids.ID] {
	transitiveClosureVotes := clone(votes)

	// Traverse from the leaves to the roots and recursively add the number of votes of descendents to each parent.
	graph.topologicalSortTraversal(func(v *voteVertex) {
		if v.parent == nil {
			return
		}
		transitiveClosureVotes.AddCount(v.parent.id, transitiveClosureVotes.Count(v.id))
	})

	return transitiveClosureVotes
}
