// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type state struct {
	serializer *Serializer

	dbCache cache.Cacher
	db      database.Database
}

// Returns the vertex whose ID is [id], or nil if it's not found
// Only returns a non-nil error if there is an unexpected value in the cache
func (s *state) Vertex(id ids.ID) (*innerVertex, error) {
	if vtxIntf, found := s.dbCache.Get(id); found {
		vtx, ok := vtxIntf.(*innerVertex)
		if !ok {
			return nil, fmt.Errorf("expected *innerVertex but got %T", vtxIntf)
		}
		return vtx, nil
	}

	if b, err := s.db.Get(id.Bytes()); err == nil {
		// The key was in the database
		if vtx, err := s.serializer.parseVertex(b); err == nil {
			s.dbCache.Put(id, vtx) // Cache the element
			return vtx, nil
		}
		s.serializer.ctx.Log.Error("Parsing failed on saved vertex.\nPrefixed key = %s\nBytes = %s",
			id,
			formatting.DumpBytes{Bytes: b})
	}
	return nil, nil
}

// SetVertex persists the vertex to the database and returns an error if it
// fails to write to the db
func (s *state) SetVertex(id ids.ID, vtx *innerVertex) error {
	s.dbCache.Put(id, vtx)

	if vtx == nil {
		return s.db.Delete(id.Bytes())
	}

	return s.db.Put(id.Bytes(), vtx.bytes)
}

func (s *state) Status(id ids.ID) (choices.Status, error) {
	if statusIntf, found := s.dbCache.Get(id); found {
		status, ok := statusIntf.(choices.Status)
		if !ok {
			return choices.Unknown, fmt.Errorf("expected status but got %T", statusIntf)
		}
		return status, nil
	}

	if b, err := s.db.Get(id.Bytes()); err == nil {
		// The key was in the database
		p := wrappers.Packer{Bytes: b}
		status := choices.Status(p.UnpackInt())
		if p.Offset == len(b) && !p.Errored() {
			s.dbCache.Put(id, status)
			return status, nil
		}
		s.serializer.ctx.Log.Error("Parsing failed on saved status.\nPrefixed key = %s\nBytes = \n%s",
			id,
			formatting.DumpBytes{Bytes: b})
	}

	s.dbCache.Put(id, choices.Unknown)
	return choices.Unknown, errUnknownVertex
}

// SetStatus sets the status of the vertex and returns an error if it fails to write to the db
func (s *state) SetStatus(id ids.ID, status choices.Status) error {
	s.dbCache.Put(id, status)

	if status == choices.Unknown {
		return s.db.Delete(id.Bytes())
	}

	p := wrappers.Packer{Bytes: make([]byte, 4)}

	p.PackInt(uint32(status))

	s.serializer.ctx.Log.AssertNoError(p.Err)
	s.serializer.ctx.Log.AssertTrue(p.Offset == len(p.Bytes), "Wrong offset after packing")

	return s.db.Put(id.Bytes(), p.Bytes)
}

// Returns the accepted frontier, or nil if there is none
// Only returns an error if the cache has an unexpected value
func (s *state) Edge(id ids.ID) ([]ids.ID, error) {
	if frontierIntf, found := s.dbCache.Get(id); found {
		frontier, ok := frontierIntf.([]ids.ID)
		if !ok {
			return nil, fmt.Errorf("expected []ids.ID but got %T", frontierIntf)
		}
		return frontier, nil
	}

	if b, err := s.db.Get(id.Bytes()); err == nil {
		p := wrappers.Packer{Bytes: b}

		frontier := []ids.ID{}
		for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
			id, err := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
			if err != nil {
				return nil, fmt.Errorf("couldn't parse ID: %w", err)
			}
			frontier = append(frontier, id)
		}

		if p.Offset == len(b) && !p.Errored() {
			s.dbCache.Put(id, frontier)
			return frontier, nil
		}
		s.serializer.ctx.Log.Error("Parsing failed on saved ids.\nPrefixed key = %s\nBytes = %s",
			id,
			formatting.DumpBytes{Bytes: b})
	}
	return nil, nil
}

// SetEdge sets the frontier and returns an error if it fails to write to the db
func (s *state) SetEdge(id ids.ID, frontier []ids.ID) error {
	s.dbCache.Put(id, frontier)

	if len(frontier) == 0 {
		return s.db.Delete(id.Bytes())
	}

	size := wrappers.IntLen + hashing.HashLen*len(frontier)
	p := wrappers.Packer{Bytes: make([]byte, size)}

	p.PackInt(uint32(len(frontier)))
	for _, id := range frontier {
		p.PackFixedBytes(id.Bytes())
	}

	s.serializer.ctx.Log.AssertNoError(p.Err)
	s.serializer.ctx.Log.AssertTrue(p.Offset == len(p.Bytes), "Wrong offset after packing")

	return s.db.Put(id.Bytes(), p.Bytes)
}
