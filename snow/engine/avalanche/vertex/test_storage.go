// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

var (
	errGetVertex = errors.New("unexpectedly called GetVertex")
	errEdge      = errors.New("unexpectedly called Edge")
)

type TestStorage struct {
	T                       *testing.T
	CantGetVertex, CantEdge bool
	GetVertexF              func(ids.ID) (avalanche.Vertex, error)
	EdgeF                   func() []ids.ID
}

func (s *TestStorage) Default(cant bool) {
	s.CantGetVertex = cant
	s.CantEdge = cant
}

func (s *TestStorage) GetVertex(id ids.ID) (avalanche.Vertex, error) {
	if s.GetVertexF != nil {
		return s.GetVertexF(id)
	}
	if s.CantGetVertex && s.T != nil {
		s.T.Fatal(errGetVertex)
	}
	return nil, errGetVertex
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
