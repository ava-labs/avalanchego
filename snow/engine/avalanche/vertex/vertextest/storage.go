// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertextest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
)

var (
	errGet                = errors.New("unexpectedly called Get")
	errEdge               = errors.New("unexpectedly called Edge")
	errStopVertexAccepted = errors.New("unexpectedly called StopVertexAccepted")

	_ vertex.Storage = (*Storage)(nil)
)

type Storage struct {
	T                                            *testing.T
	CantGetVtx, CantEdge, CantStopVertexAccepted bool
	GetVtxF                                      func(context.Context, ids.ID) (avalanche.Vertex, error)
	EdgeF                                        func(context.Context) []ids.ID
	StopVertexAcceptedF                          func(context.Context) (bool, error)
}

func (s *Storage) Default(cant bool) {
	s.CantGetVtx = cant
	s.CantEdge = cant
}

func (s *Storage) GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
	if s.GetVtxF != nil {
		return s.GetVtxF(ctx, vtxID)
	}
	if s.CantGetVtx && s.T != nil {
		require.FailNow(s.T, errGet.Error())
	}
	return nil, errGet
}

func (s *Storage) Edge(ctx context.Context) []ids.ID {
	if s.EdgeF != nil {
		return s.EdgeF(ctx)
	}
	if s.CantEdge && s.T != nil {
		require.FailNow(s.T, errEdge.Error())
	}
	return nil
}

func (s *Storage) StopVertexAccepted(ctx context.Context) (bool, error) {
	if s.StopVertexAcceptedF != nil {
		return s.StopVertexAcceptedF(ctx)
	}
	if s.CantStopVertexAccepted && s.T != nil {
		require.FailNow(s.T, errStopVertexAccepted.Error())
	}
	return false, nil
}
