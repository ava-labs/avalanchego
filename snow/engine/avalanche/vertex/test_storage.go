// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

var (
	errGet                = errors.New("unexpectedly called Get")
	errEdge               = errors.New("unexpectedly called Edge")
	errStopVertexAccepted = errors.New("unexpectedly called StopVertexAccepted")

	_ Storage = (*TestStorage)(nil)
)

type TestStorage struct {
	T                                            *testing.T
	CantGetVtx, CantEdge, CantStopVertexAccepted bool
	GetVtxF                                      func(context.Context, ids.ID) (avalanche.Vertex, error)
	EdgeF                                        func(context.Context) []ids.ID
	StopVertexAcceptedF                          func(context.Context) (bool, error)
}

func (s *TestStorage) Default(cant bool) {
	s.CantGetVtx = cant
	s.CantEdge = cant
}

func (s *TestStorage) GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
	if s.GetVtxF != nil {
		return s.GetVtxF(ctx, vtxID)
	}
	if s.CantGetVtx && s.T != nil {
		s.T.Fatal(errGet)
	}
	return nil, errGet
}

func (s *TestStorage) Edge(ctx context.Context) []ids.ID {
	if s.EdgeF != nil {
		return s.EdgeF(ctx)
	}
	if s.CantEdge && s.T != nil {
		s.T.Fatal(errEdge)
	}
	return nil
}

func (s *TestStorage) StopVertexAccepted(ctx context.Context) (bool, error) {
	if s.StopVertexAcceptedF != nil {
		return s.StopVertexAcceptedF(ctx)
	}
	if s.CantStopVertexAccepted && s.T != nil {
		s.T.Fatal(errStopVertexAccepted)
	}
	return false, nil
}
