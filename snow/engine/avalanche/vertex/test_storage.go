// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
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
	GetVtxF                                      func(ids.ID) (avalanche.Vertex, error)
	EdgeF                                        func() []ids.ID
	StopVertexAcceptedF                          func() (bool, error)
}

func (s *TestStorage) Default(cant bool) {
	s.CantGetVtx = cant
	s.CantEdge = cant
}

func (s *TestStorage) GetVtx(id ids.ID) (avalanche.Vertex, error) {
	if s.GetVtxF != nil {
		return s.GetVtxF(id)
	}
	if s.CantGetVtx && s.T != nil {
		s.T.Fatal(errGet)
	}
	return nil, errGet
}

func (s *TestStorage) Edge() []ids.ID {
	if s.EdgeF != nil {
		return s.EdgeF()
	}
	if s.CantEdge && s.T != nil {
		s.T.Fatal(errEdge)
	}
	return nil
}

func (s *TestStorage) StopVertexAccepted() (bool, error) {
	if s.StopVertexAcceptedF != nil {
		return s.StopVertexAcceptedF()
	}
	if s.CantStopVertexAccepted && s.T != nil {
		s.T.Fatal(errStopVertexAccepted)
	}
	return false, nil
}
