// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var errUnknownTx = errors.New("unknown tx")

func newTestSerializer(t *testing.T, parse func(context.Context, []byte) (snowstorm.Tx, error)) *Serializer {
	vm := vertex.TestVM{}
	vm.T = t
	vm.Default(true)
	vm.ParseTxF = parse

	baseDB := memdb.New()
	ctx := snow.DefaultContextTest()
	s := NewSerializer(
		SerializerConfig{
			ChainID: ctx.ChainID,
			VM:      &vm,
			DB:      baseDB,
			Log:     ctx.Log,
		},
	)

	return s.(*Serializer)
}

func TestUnknownUniqueVertexErrors(t *testing.T) {
	s := newTestSerializer(t, nil)

	uVtx := &uniqueVertex{
		serializer: s,
		id:         ids.ID{},
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

	_, err = uVtx.Txs(context.Background())
	if err == nil {
		t.Fatalf("Txs should have produced an error for unknown vertex")
	}
}

func TestUniqueVertexCacheHit(t *testing.T) {
	testTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV: ids.ID{1},
	}}

	s := newTestSerializer(t, func(_ context.Context, b []byte) (snowstorm.Tx, error) {
		if !bytes.Equal(b, []byte{0}) {
			t.Fatal("unknown tx")
		}
		return testTx, nil
	})

	id := ids.ID{2}
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't'}
	parentIDs := []ids.ID{parentID}
	chainID := ids.ID{} // Same as chainID of serializer
	height := uint64(1)
	vtx, err := vertex.Build( // regular, non-stop vertex
		chainID,
		height,
		parentIDs,
		[][]byte{{0}},
	)
	if err != nil {
		t.Fatal(err)
	}

	uVtx := &uniqueVertex{
		id:         id,
		serializer: s,
	}
	if err := uVtx.setVertex(context.Background(), vtx); err != nil {
		t.Fatalf("Failed to set vertex due to: %s", err)
	}

	newUVtx := &uniqueVertex{
		id:         id,
		serializer: s,
	}

	parents, err := newUVtx.Parents()
	if err != nil {
		t.Fatalf("Error while retrieving parents of known vertex")
	}
	if len(parents) != 1 {
		t.Fatalf("Parents should have length 1")
	}
	if parents[0].ID() != parentID {
		t.Fatalf("ParentID is incorrect")
	}

	newHeight, err := newUVtx.Height()
	if err != nil {
		t.Fatalf("Error while retrieving height of known vertex")
	}
	if height != newHeight {
		t.Fatalf("Vertex height should have been %d, but was: %d", height, newHeight)
	}

	txs, err := newUVtx.Txs(context.Background())
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

func TestUniqueVertexCacheMiss(t *testing.T) {
	txBytesParent := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	testTxParent := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{1},
			StatusV: choices.Accepted,
		},
		BytesV: txBytesParent,
	}

	txBytes := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	testTx := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV: ids.ID{1},
		},
		BytesV: txBytes,
	}
	parseTx := func(_ context.Context, b []byte) (snowstorm.Tx, error) {
		if bytes.Equal(txBytesParent, b) {
			return testTxParent, nil
		}
		if bytes.Equal(txBytes, b) {
			return testTx, nil
		}
		t.Fatal("asked to parse unexpected transaction")
		return nil, nil
	}

	s := newTestSerializer(t, parseTx)

	uvtxParent := newTestUniqueVertex(t, s, nil, [][]byte{txBytesParent}, false)
	if err := uvtxParent.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	parentID := uvtxParent.ID()
	parentIDs := []ids.ID{parentID}
	chainID := ids.ID{}
	height := uint64(1)
	innerVertex, err := vertex.Build( // regular, non-stop vertex
		chainID,
		height,
		parentIDs,
		[][]byte{txBytes},
	)
	if err != nil {
		t.Fatal(err)
	}

	id := innerVertex.ID()
	vtxBytes := innerVertex.Bytes()

	uVtx := uniqueVertex{
		id:         id,
		serializer: s,
	}

	// Register a cache miss
	if status := uVtx.Status(); status != choices.Unknown {
		t.Fatalf("expected status to be unknown, but found: %s", status)
	}

	// Register cache hit
	vtx, err := newUniqueVertex(context.Background(), s, vtxBytes)
	if err != nil {
		t.Fatal(err)
	}

	if status := vtx.Status(); status != choices.Processing {
		t.Fatalf("expected status to be processing, but found: %s", status)
	}

	validateVertex := func(vtx *uniqueVertex, expectedStatus choices.Status) {
		if status := vtx.Status(); status != expectedStatus {
			t.Fatalf("expected status to be %s, but found: %s", expectedStatus, status)
		}

		// Call bytes first to check for regression bug
		// where it's unsafe to call Bytes or Verify directly
		// after calling Status to refresh a vertex
		if !bytes.Equal(vtx.Bytes(), vtxBytes) {
			t.Fatalf("Found unexpected vertex bytes")
		}

		vtxParents, err := vtx.Parents()
		if err != nil {
			t.Fatalf("Fetching vertex parents errored with: %s", err)
		}
		vtxHeight, err := vtx.Height()
		if err != nil {
			t.Fatalf("Fetching vertex height errored with: %s", err)
		}
		vtxTxs, err := vtx.Txs(context.Background())
		if err != nil {
			t.Fatalf("Fetching vertx txs errored with: %s", err)
		}
		switch {
		case vtxHeight != height:
			t.Fatalf("Expected vertex height to be %d, but found %d", height, vtxHeight)
		case len(vtxParents) != 1:
			t.Fatalf("Expected vertex to have 1 parent, but found %d", len(vtxParents))
		case vtxParents[0].ID() != parentID:
			t.Fatalf("Found unexpected parentID: %s, expected: %s", vtxParents[0].ID(), parentID)
		case len(vtxTxs) != 1:
			t.Fatalf("Exepcted vertex to have 1 transaction, but found %d", len(vtxTxs))
		case !bytes.Equal(vtxTxs[0].Bytes(), txBytes):
			t.Fatalf("Found unexpected transaction bytes")
		}
	}

	// Replace the vertex, so that it loses reference to parents, etc.
	vtx = &uniqueVertex{
		id:         id,
		serializer: s,
	}

	// Check that the vertex refreshed from the cache is valid
	validateVertex(vtx, choices.Processing)

	// Check that a newly parsed vertex refreshed from the cache is valid
	vtx, err = newUniqueVertex(context.Background(), s, vtxBytes)
	if err != nil {
		t.Fatal(err)
	}
	validateVertex(vtx, choices.Processing)

	// Check that refreshing a vertex when it has been removed from
	// the cache works correctly

	s.state.uniqueVtx.Flush()
	vtx = &uniqueVertex{
		id:         id,
		serializer: s,
	}
	validateVertex(vtx, choices.Processing)

	s.state.uniqueVtx.Flush()
	vtx, err = newUniqueVertex(context.Background(), s, vtxBytes)
	if err != nil {
		t.Fatal(err)
	}
	validateVertex(vtx, choices.Processing)
}

func TestParseVertexWithIncorrectChainID(t *testing.T) {
	statelessVertex, err := vertex.Build( // regular, non-stop vertex
		ids.GenerateTestID(),
		0,
		nil,
		[][]byte{{1}},
	)
	if err != nil {
		t.Fatal(err)
	}
	vtxBytes := statelessVertex.Bytes()

	s := newTestSerializer(t, func(_ context.Context, b []byte) (snowstorm.Tx, error) {
		if bytes.Equal(b, []byte{1}) {
			return &snowstorm.TestTx{}, nil
		}
		return nil, errUnknownTx
	})

	if _, err := s.ParseVtx(context.Background(), vtxBytes); err == nil {
		t.Fatal("should have failed to parse the vertex due to invalid chainID")
	}
}

func TestParseVertexWithInvalidTxs(t *testing.T) {
	ctx := snow.DefaultContextTest()
	statelessVertex, err := vertex.Build( // regular, non-stop vertex
		ctx.ChainID,
		0,
		nil,
		[][]byte{{1}},
	)
	if err != nil {
		t.Fatal(err)
	}
	vtxBytes := statelessVertex.Bytes()

	s := newTestSerializer(t, func(_ context.Context, b []byte) (snowstorm.Tx, error) {
		switch {
		case bytes.Equal(b, []byte{2}):
			return &snowstorm.TestTx{}, nil
		default:
			return nil, errUnknownTx
		}
	})

	if _, err := s.ParseVtx(context.Background(), vtxBytes); err == nil {
		t.Fatal("should have failed to parse the vertex due to invalid transactions")
	}

	if _, err := s.ParseVtx(context.Background(), vtxBytes); err == nil {
		t.Fatal("should have failed to parse the vertex after previously error on parsing invalid transactions")
	}

	id := hashing.ComputeHash256Array(vtxBytes)
	if _, err := s.GetVtx(context.Background(), id); err == nil {
		t.Fatal("should have failed to lookup invalid vertex after previously error on parsing invalid transactions")
	}

	childStatelessVertex, err := vertex.Build( // regular, non-stop vertex
		ctx.ChainID,
		1,
		[]ids.ID{id},
		[][]byte{{2}},
	)
	if err != nil {
		t.Fatal(err)
	}
	childVtxBytes := childStatelessVertex.Bytes()

	childVtx, err := s.ParseVtx(context.Background(), childVtxBytes)
	if err != nil {
		t.Fatal(err)
	}

	parents, err := childVtx.Parents()
	if err != nil {
		t.Fatal(err)
	}
	if len(parents) != 1 {
		t.Fatal("wrong number of parents")
	}
	parent := parents[0]

	if parent.Status().Fetched() {
		t.Fatal("the parent is invalid, so it shouldn't be marked as fetched")
	}
}

func newTestUniqueVertex(
	t *testing.T,
	s *Serializer,
	parentIDs []ids.ID,
	txs [][]byte,
	stopVertex bool,
) *uniqueVertex {
	var (
		vtx vertex.StatelessVertex
		err error
	)
	if !stopVertex {
		vtx, err = vertex.Build(
			ids.ID{},
			uint64(1),
			parentIDs,
			txs,
		)
	} else {
		vtx, err = vertex.BuildStopVertex(
			ids.ID{},
			uint64(1),
			parentIDs,
		)
	}
	if err != nil {
		t.Fatal(err)
	}
	uvtx, err := newUniqueVertex(context.Background(), s, vtx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return uvtx
}
