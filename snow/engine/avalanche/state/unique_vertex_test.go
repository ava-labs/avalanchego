// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex/vertextest"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var errUnknownTx = errors.New("unknown tx")

func newTestSerializer(t *testing.T, parse func(context.Context, []byte) (snowstorm.Tx, error)) *Serializer {
	vm := vertextest.VM{}
	vm.T = t
	vm.Default(true)
	vm.ParseTxF = parse

	baseDB := memdb.New()
	s := NewSerializer(
		SerializerConfig{
			ChainID: ids.Empty,
			VM:      &vm,
			DB:      baseDB,
			Log:     logging.NoLog{},
		},
	)

	return s.(*Serializer)
}

func TestUnknownUniqueVertexErrors(t *testing.T) {
	require := require.New(t)
	s := newTestSerializer(t, nil)

	uVtx := &uniqueVertex{
		serializer: s,
		id:         ids.Empty,
	}

	status := uVtx.Status()
	require.Equal(choices.Unknown, status)

	_, err := uVtx.Parents()
	require.ErrorIs(err, errGetParents)

	_, err = uVtx.Height()
	require.ErrorIs(err, errGetHeight)

	_, err = uVtx.Txs(t.Context())
	require.ErrorIs(err, errGetTxs)
}

func TestUniqueVertexCacheHit(t *testing.T) {
	require := require.New(t)

	testTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV: ids.ID{1},
	}}

	s := newTestSerializer(t, func(_ context.Context, b []byte) (snowstorm.Tx, error) {
		require.Equal([]byte{0}, b)
		return testTx, nil
	})

	id := ids.ID{2}
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't'}
	parentIDs := []ids.ID{parentID}
	height := uint64(1)
	vtx, err := vertex.Build( // regular, non-stop vertex
		s.ChainID,
		height,
		parentIDs,
		[][]byte{{0}},
	)
	require.NoError(err)

	uVtx := &uniqueVertex{
		id:         id,
		serializer: s,
	}
	require.NoError(uVtx.setVertex(t.Context(), vtx))

	newUVtx := &uniqueVertex{
		id:         id,
		serializer: s,
	}

	parents, err := newUVtx.Parents()
	require.NoError(err)
	require.Len(parents, 1)
	require.Equal(parentID, parents[0].ID())

	newHeight, err := newUVtx.Height()
	require.NoError(err)
	require.Equal(height, newHeight)

	txs, err := newUVtx.Txs(t.Context())
	require.NoError(err)
	require.Len(txs, 1)
	require.Equal(testTx, txs[0])

	require.Equal(uVtx.v, newUVtx.v)
}

func TestUniqueVertexCacheMiss(t *testing.T) {
	require := require.New(t)

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
		require.FailNow("asked to parse unexpected transaction")
		return nil, nil
	}

	s := newTestSerializer(t, parseTx)

	uvtxParent := newTestUniqueVertex(t, s, nil, [][]byte{txBytesParent}, false)
	require.NoError(uvtxParent.Accept(t.Context()))

	parentID := uvtxParent.ID()
	parentIDs := []ids.ID{parentID}
	height := uint64(1)
	innerVertex, err := vertex.Build( // regular, non-stop vertex
		s.ChainID,
		height,
		parentIDs,
		[][]byte{txBytes},
	)
	require.NoError(err)

	id := innerVertex.ID()
	vtxBytes := innerVertex.Bytes()

	uVtx := uniqueVertex{
		id:         id,
		serializer: s,
	}

	// Register a cache miss
	require.Equal(choices.Unknown, uVtx.Status())

	// Register cache hit
	vtx, err := newUniqueVertex(t.Context(), s, vtxBytes)
	require.NoError(err)

	require.Equal(choices.Processing, vtx.Status())

	validateVertex := func(vtx *uniqueVertex, expectedStatus choices.Status) {
		require.Equal(expectedStatus, vtx.Status())

		// Call bytes first to check for regression bug
		// where it's unsafe to call Bytes or Verify directly
		// after calling Status to refresh a vertex
		require.Equal(vtxBytes, vtx.Bytes())

		vtxParents, err := vtx.Parents()
		require.NoError(err)
		require.Len(vtxParents, 1)
		require.Equal(parentID, vtxParents[0].ID())

		vtxHeight, err := vtx.Height()
		require.NoError(err)
		require.Equal(height, vtxHeight)

		vtxTxs, err := vtx.Txs(t.Context())
		require.NoError(err)
		require.Len(vtxTxs, 1)
		require.Equal(txBytes, vtxTxs[0].Bytes())
	}

	// Replace the vertex, so that it loses reference to parents, etc.
	vtx = &uniqueVertex{
		id:         id,
		serializer: s,
	}

	// Check that the vertex refreshed from the cache is valid
	validateVertex(vtx, choices.Processing)

	// Check that a newly parsed vertex refreshed from the cache is valid
	vtx, err = newUniqueVertex(t.Context(), s, vtxBytes)
	require.NoError(err)
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
	vtx, err = newUniqueVertex(t.Context(), s, vtxBytes)
	require.NoError(err)
	validateVertex(vtx, choices.Processing)
}

func TestParseVertexWithIncorrectChainID(t *testing.T) {
	require := require.New(t)

	statelessVertex, err := vertex.Build( // regular, non-stop vertex
		ids.GenerateTestID(),
		0,
		nil,
		[][]byte{{1}},
	)
	require.NoError(err)
	vtxBytes := statelessVertex.Bytes()

	s := newTestSerializer(t, func(_ context.Context, b []byte) (snowstorm.Tx, error) {
		if bytes.Equal(b, []byte{1}) {
			return &snowstorm.TestTx{}, nil
		}
		return nil, errUnknownTx
	})

	_, err = s.ParseVtx(t.Context(), vtxBytes)
	require.ErrorIs(err, errWrongChainID)
}

func TestParseVertexWithInvalidTxs(t *testing.T) {
	require := require.New(t)

	s := newTestSerializer(t, func(_ context.Context, b []byte) (snowstorm.Tx, error) {
		switch {
		case bytes.Equal(b, []byte{2}):
			return &snowstorm.TestTx{}, nil
		default:
			return nil, errUnknownTx
		}
	})

	statelessVertex, err := vertex.Build( // regular, non-stop vertex
		s.ChainID,
		0,
		nil,
		[][]byte{{1}},
	)
	require.NoError(err)
	vtxBytes := statelessVertex.Bytes()

	_, err = s.ParseVtx(t.Context(), vtxBytes)
	require.ErrorIs(err, errUnknownTx)

	_, err = s.ParseVtx(t.Context(), vtxBytes)
	require.ErrorIs(err, errUnknownTx)

	id := hashing.ComputeHash256Array(vtxBytes)
	_, err = s.GetVtx(t.Context(), id)
	require.ErrorIs(err, errUnknownVertex)

	childStatelessVertex, err := vertex.Build( // regular, non-stop vertex
		s.ChainID,
		1,
		[]ids.ID{id},
		[][]byte{{2}},
	)
	require.NoError(err)
	childVtxBytes := childStatelessVertex.Bytes()

	childVtx, err := s.ParseVtx(t.Context(), childVtxBytes)
	require.NoError(err)

	parents, err := childVtx.Parents()
	require.NoError(err)
	require.Len(parents, 1)
	parent := parents[0]

	require.False(parent.Status().Fetched())
}

func newTestUniqueVertex(
	t *testing.T,
	s *Serializer,
	parentIDs []ids.ID,
	txs [][]byte,
	stopVertex bool,
) *uniqueVertex {
	require := require.New(t)

	var (
		vtx vertex.StatelessVertex
		err error
	)
	if !stopVertex {
		vtx, err = vertex.Build(
			s.ChainID,
			uint64(1),
			parentIDs,
			txs,
		)
	} else {
		vtx, err = vertex.BuildStopVertex(
			s.ChainID,
			uint64(1),
			parentIDs,
		)
	}
	require.NoError(err)
	uvtx, err := newUniqueVertex(t.Context(), s, vtx.Bytes())
	require.NoError(err)
	return uvtx
}
