// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
)

const (
	vtxID uint64 = iota
	vtxStatusID
	edgeID
)

var (
	uniqueEdgeID = ids.Empty.Prefix(edgeID)
)

type prefixedState struct {
	state *state

	vtx, status cache.Cacher
	uniqueVtx   cache.Deduplicator
}

func newPrefixedState(state *state, idCacheSizes int) *prefixedState {
	return &prefixedState{
		state:     state,
		vtx:       &cache.LRU{Size: idCacheSizes},
		status:    &cache.LRU{Size: idCacheSizes},
		uniqueVtx: &cache.EvictableLRU{Size: idCacheSizes},
	}
}

func (s *prefixedState) UniqueVertex(vtx *uniqueVertex) *uniqueVertex {
	return s.uniqueVtx.Deduplicate(vtx).(*uniqueVertex)
}

func (s *prefixedState) Vertex(id ids.ID) *innerVertex {
	vID := ids.ID{}
	if cachedVtxIDIntf, found := s.vtx.Get(id); found {
		vID = cachedVtxIDIntf.(ids.ID)
	} else {
		vID = id.Prefix(vtxID)
		s.vtx.Put(id, vID)
	}

	return s.state.Vertex(vID)
}

func (s *prefixedState) SetVertex(vtx *innerVertex) {
	vID := ids.ID{}
	if cachedVtxIDIntf, found := s.vtx.Get(vtx.id); found {
		vID = cachedVtxIDIntf.(ids.ID)
	} else {
		vID = vtx.id.Prefix(vtxID)
		s.vtx.Put(vtx.id, vID)
	}

	s.state.SetVertex(vID, vtx)
}

func (s *prefixedState) Status(id ids.ID) choices.Status {
	sID := ids.ID{}
	if cachedStatusIDIntf, found := s.status.Get(id); found {
		sID = cachedStatusIDIntf.(ids.ID)
	} else {
		sID = id.Prefix(vtxStatusID)
		s.status.Put(id, sID)
	}

	return s.state.Status(sID)
}

func (s *prefixedState) SetStatus(id ids.ID, status choices.Status) {
	sID := ids.ID{}
	if cachedStatusIDIntf, found := s.status.Get(id); found {
		sID = cachedStatusIDIntf.(ids.ID)
	} else {
		sID = id.Prefix(vtxStatusID)
		s.status.Put(id, sID)
	}

	s.state.SetStatus(sID, status)
}

func (s *prefixedState) Edge() []ids.ID { return s.state.Edge(uniqueEdgeID) }

func (s *prefixedState) SetEdge(frontier []ids.ID) { s.state.SetEdge(uniqueEdgeID, frontier) }
