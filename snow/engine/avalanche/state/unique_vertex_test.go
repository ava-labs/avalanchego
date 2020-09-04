// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/avalanche/vertex"
)

func newSerializer(t *testing.T) *Serializer {
	vm := vertex.TestVM{}
	vm.T = t
	vm.Default(true)

	baseDB := memdb.New()
	ctx := snow.DefaultContextTest()
	s := &Serializer{}
	s.Initialize(ctx, &vm, baseDB)
	return s
}

func TestUnknownUniqueVertexErrors(t *testing.T) {
	s := newSerializer(t)

	uVtx := &uniqueVertex{
		serializer: s,
		vtxID:      ids.NewID([32]byte{}),
	}

	status := uVtx.Status()
	if status != choices.Unknown {
		t.Fatalf("Expected vertex to have Unknown status")
	}

	_, err := uVtx.Parents()
	if err == nil {
		t.Fatalf("Parents should have produced error for unknown vertex")
	}

	_, err = uVtx.Height()
	if err == nil {
		t.Fatalf("Height should have produced error for unknown vertex")
	}

	_, err = uVtx.Txs()
	if err == nil {
		t.Fatalf("Txs should have produced an error for unknown vertex")
	}
}

func TestUniqueVertexCacheHit(t *testing.T) {
	s := newSerializer(t)

	testTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV: ids.NewID([32]byte{1}),
	}}

	vtxID := ids.NewID([32]byte{2})
	parentID := ids.NewID([32]byte{'p', 'a', 'r', 'e', 'n', 't'})
	parentIDs := []ids.ID{parentID}
	chainID := ids.NewID([32]byte{})
	height := uint64(1)
	vtx := &innerVertex{
		id:        vtxID,
		parentIDs: parentIDs,
		chainID:   chainID,
		height:    height,
		txs:       []snowstorm.Tx{testTx},
	}

	uVtx := &uniqueVertex{
		vtxID:      vtxID,
		serializer: s,
	}
	if err := uVtx.setVertex(vtx); err != nil {
		t.Fatalf("Failed to set vertex due to: %s", err)
	}

	newUVtx := &uniqueVertex{
		vtxID:      vtxID,
		serializer: s,
	}

	parents, err := newUVtx.Parents()
	if err != nil {
		t.Fatalf("Error while retrieving parents of known vertex")
	}
	if len(parents) != 1 {
		t.Fatalf("Parents should have length 1")
	}
	if !parents[0].ID().Equals(parentID) {
		t.Fatalf("ParentID is incorrect")
	}

	newHeight, err := newUVtx.Height()
	if err != nil {
		t.Fatalf("Error while retrieving height of known vertex")
	}
	if height != newHeight {
		t.Fatalf("Vertex height should have been %d, but was: %d", height, newHeight)
	}

	txs, err := newUVtx.Txs()
	if err != nil {
		t.Fatalf("Error while retrieving txs of known vertex: %s", err)
	}
	if len(txs) != 1 {
		t.Fatalf("Incorrect number of transactions")
	}
	if txs[0] != testTx {
		t.Fatalf("Txs retrieved the wrong Tx")
	}

	if newUVtx.v != uVtx.v {
		t.Fatalf("Unique vertex failed to get corresponding vertex state from cache")
	}
}
