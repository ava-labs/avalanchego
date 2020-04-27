// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package state manages the meta-data required by consensus for an avalanche
// dag.
package state

import (
	"errors"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/math"

	avacon "github.com/ava-labs/gecko/snow/consensus/avalanche"
	avaeng "github.com/ava-labs/gecko/snow/engine/avalanche"
)

const (
	dbCacheSize = 10000
	idCacheSize = 1000
)

var (
	errUnknownVertex = errors.New("unknown vertex")
	errWrongChainID  = errors.New("wrong ChainID in vertex")
)

// Serializer manages the state of multiple vertices
type Serializer struct {
	ctx   *snow.Context
	vm    avaeng.DAGVM
	state *prefixedState
	db    *versiondb.Database
	edge  ids.Set
}

// Initialize implements the avalanche.State interface
func (s *Serializer) Initialize(ctx *snow.Context, vm avaeng.DAGVM, db database.Database) {
	s.ctx = ctx
	s.vm = vm

	vdb := versiondb.New(db)
	dbCache := &cache.LRU{Size: dbCacheSize}
	rawState := &state{
		serializer: s,
		dbCache:    dbCache,
		db:         vdb,
	}
	s.state = newPrefixedState(rawState, idCacheSize)
	s.db = vdb

	s.edge.Add(s.state.Edge()...)
}

// ParseVertex implements the avalanche.State interface
func (s *Serializer) ParseVertex(b []byte) (avacon.Vertex, error) {
	vtx, err := s.parseVertex(b)
	if err != nil {
		return nil, err
	}
	if err := vtx.Verify(); err != nil {
		return nil, err
	}
	uVtx := &uniqueVertex{
		serializer: s,
		vtxID:      vtx.ID(),
	}
	if uVtx.Status() == choices.Unknown {
		uVtx.setVertex(vtx)
	}

	s.db.Commit()
	return uVtx, nil
}

// BuildVertex implements the avalanche.State interface
func (s *Serializer) BuildVertex(parentSet ids.Set, txs []snowstorm.Tx) (avacon.Vertex, error) {
	if len(txs) == 0 {
		return nil, errNoTxs
	}

	parentIDs := parentSet.List()
	ids.SortIDs(parentIDs)
	sortTxs(txs)

	height := uint64(0)
	for _, parentID := range parentIDs {
		parent, err := s.getVertex(parentID)
		if err != nil {
			return nil, err
		}
		height = math.Max64(height, parent.v.vtx.height)
	}

	vtx := &vertex{
		chainID:   s.ctx.ChainID,
		height:    height + 1,
		parentIDs: parentIDs,
		txs:       txs,
	}

	bytes, err := vtx.Marshal()
	if err != nil {
		return nil, err
	}
	vtx.bytes = bytes
	vtx.id = ids.NewID(hashing.ComputeHash256Array(vtx.bytes))

	uVtx := &uniqueVertex{
		serializer: s,
		vtxID:      vtx.ID(),
	}
	// It is possible this vertex already exists in the database, even though we
	// just made it.
	if uVtx.Status() == choices.Unknown {
		uVtx.setVertex(vtx)
	}

	s.db.Commit()
	return uVtx, nil
}

// GetVertex implements the avalanche.State interface
func (s *Serializer) GetVertex(vtxID ids.ID) (avacon.Vertex, error) { return s.getVertex(vtxID) }

// Edge implements the avalanche.State interface
func (s *Serializer) Edge() []ids.ID { return s.edge.List() }

func (s *Serializer) parseVertex(b []byte) (*vertex, error) {
	vtx := &vertex{}
	if err := vtx.Unmarshal(b, s.vm); err != nil {
		return nil, err
	} else if !vtx.chainID.Equals(s.ctx.ChainID) {
		return nil, errWrongChainID
	}
	return vtx, nil
}

func (s *Serializer) getVertex(vtxID ids.ID) (*uniqueVertex, error) {
	vtx := &uniqueVertex{
		serializer: s,
		vtxID:      vtxID,
	}
	if vtx.Status() == choices.Unknown {
		return nil, errUnknownVertex
	}
	return vtx, nil
}
