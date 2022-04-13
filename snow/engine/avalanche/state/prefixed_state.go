// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/chain4travel/caminogo/cache"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
	"github.com/chain4travel/caminogo/snow/engine/avalanche/vertex"
)

const (
	vtxID uint64 = iota
	vtxStatusID
	edgeID
)

var uniqueEdgeID = ids.Empty.Prefix(edgeID)

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

func (s *prefixedState) Vertex(id ids.ID) vertex.StatelessVertex {
	var vID ids.ID
	if cachedVtxIDIntf, found := s.vtx.Get(id); found {
		vID = cachedVtxIDIntf.(ids.ID)
	} else {
		vID = id.Prefix(vtxID)
		s.vtx.Put(id, vID)
	}

	return s.state.Vertex(vID)
}

func (s *prefixedState) SetVertex(vtx vertex.StatelessVertex) error {
	rawVertexID := vtx.ID()
	var vID ids.ID
	if cachedVtxIDIntf, found := s.vtx.Get(rawVertexID); found {
		vID = cachedVtxIDIntf.(ids.ID)
	} else {
		vID = rawVertexID.Prefix(vtxID)
		s.vtx.Put(rawVertexID, vID)
	}

	return s.state.SetVertex(vID, vtx)
}

func (s *prefixedState) Status(id ids.ID) choices.Status {
	var sID ids.ID
	if cachedStatusIDIntf, found := s.status.Get(id); found {
		sID = cachedStatusIDIntf.(ids.ID)
	} else {
		sID = id.Prefix(vtxStatusID)
		s.status.Put(id, sID)
	}

	return s.state.Status(sID)
}

func (s *prefixedState) SetStatus(id ids.ID, status choices.Status) error {
	var sID ids.ID
	if cachedStatusIDIntf, found := s.status.Get(id); found {
		sID = cachedStatusIDIntf.(ids.ID)
	} else {
		sID = id.Prefix(vtxStatusID)
		s.status.Put(id, sID)
	}

	return s.state.SetStatus(sID, status)
}

func (s *prefixedState) Edge() []ids.ID { return s.state.Edge(uniqueEdgeID) }

func (s *prefixedState) SetEdge(frontier []ids.ID) error {
	return s.state.SetEdge(uniqueEdgeID, frontier)
}
