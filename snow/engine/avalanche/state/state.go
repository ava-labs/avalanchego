// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type state struct {
	serializer *Serializer
	log        logging.Logger

	dbCache cache.Cacher[ids.ID, any]
	db      database.Database
}

// Vertex retrieves the vertex with the given id from cache/disk.
// Returns nil if it's not found.
// TODO this should return an error
func (s *state) Vertex(id ids.ID) vertex.StatelessVertex {
	if vtxIntf, found := s.dbCache.Get(id); found {
		vtx, _ := vtxIntf.(vertex.StatelessVertex)
		return vtx
	}

	bytes, err := s.db.Get(id[:])
	if err != nil {
		s.log.Verbo("failed to get vertex from database",
			zap.Binary("key", id[:]),
			zap.Error(err),
		)
		s.dbCache.Put(id, nil)
		return nil
	}

	vtx, err := s.serializer.parseVertex(bytes)
	if err != nil {
		s.log.Error("failed parsing saved vertex",
			zap.Binary("key", id[:]),
			zap.Binary("vertex", bytes),
			zap.Error(err),
		)
		s.dbCache.Put(id, nil)
		return nil
	}

	s.dbCache.Put(id, vtx)
	return vtx
}

// SetVertex persists the vertex to the database and returns an error if it
// fails to write to the db
func (s *state) SetVertex(id ids.ID, vtx vertex.StatelessVertex) error {
	s.dbCache.Put(id, vtx)

	if vtx == nil {
		return s.db.Delete(id[:])
	}

	return s.db.Put(id[:], vtx.Bytes())
}

func (s *state) Status(id ids.ID) choices.Status {
	if statusIntf, found := s.dbCache.Get(id); found {
		status, _ := statusIntf.(choices.Status)
		return status
	}

	if val, err := database.GetUInt32(s.db, id[:]); err == nil {
		// The key was in the database
		status := choices.Status(val)
		s.dbCache.Put(id, status)
		return status
	}

	s.dbCache.Put(id, choices.Unknown)
	return choices.Unknown
}

// SetStatus sets the status of the vertex and returns an error if it fails to write to the db
func (s *state) SetStatus(id ids.ID, status choices.Status) error {
	s.dbCache.Put(id, status)

	if status == choices.Unknown {
		return s.db.Delete(id[:])
	}
	return database.PutUInt32(s.db, id[:], uint32(status))
}

func (s *state) Edge(id ids.ID) []ids.ID {
	if frontierIntf, found := s.dbCache.Get(id); found {
		frontier, _ := frontierIntf.([]ids.ID)
		return frontier
	}

	if b, err := s.db.Get(id[:]); err == nil {
		p := wrappers.Packer{Bytes: b}

		frontierSize := p.UnpackInt()
		frontier := make([]ids.ID, frontierSize)
		for i := 0; i < int(frontierSize) && !p.Errored(); i++ {
			id, err := ids.ToID(p.UnpackFixedBytes(ids.IDLen))
			p.Add(err)
			frontier[i] = id
		}

		if p.Offset == len(b) && !p.Errored() {
			s.dbCache.Put(id, frontier)
			return frontier
		}
		s.log.Error("failed parsing saved edge",
			zap.Binary("key", id[:]),
			zap.Binary("edge", b),
			zap.Error(err),
		)
	}

	s.dbCache.Put(id, nil) // Cache the miss
	return nil
}

// SetEdge sets the frontier and returns an error if it fails to write to the db
func (s *state) SetEdge(id ids.ID, frontier []ids.ID) error {
	s.dbCache.Put(id, frontier)

	if len(frontier) == 0 {
		return s.db.Delete(id[:])
	}

	size := wrappers.IntLen + ids.IDLen*len(frontier)
	p := wrappers.Packer{Bytes: make([]byte, size)}
	p.PackInt(uint32(len(frontier)))
	for _, id := range frontier {
		p.PackFixedBytes(id[:])
	}

	return s.db.Put(id[:], p.Bytes)
}
