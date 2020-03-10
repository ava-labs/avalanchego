// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"errors"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

var (
	errParseVertex = errors.New("unexpectedly called ParseVertex")
	errBuildVertex = errors.New("unexpectedly called BuildVertex")
	errGetVertex   = errors.New("unexpectedly called GetVertex")
)

type stateTest struct {
	t *testing.T

	cantParseVertex, cantBuildVertex, cantGetVertex, cantEdge, cantSaveEdge bool

	parseVertex func([]byte) (avalanche.Vertex, error)
	buildVertex func(ids.Set, []snowstorm.Tx) (avalanche.Vertex, error)
	getVertex   func(ids.ID) (avalanche.Vertex, error)

	edge     func() []ids.ID
	saveEdge func([]ids.ID)
}

func (s *stateTest) Default(cant bool) {
	s.cantParseVertex = cant
	s.cantBuildVertex = cant
	s.cantGetVertex = cant
	s.cantEdge = cant
	s.cantSaveEdge = cant
}

func (s *stateTest) ParseVertex(b []byte) (avalanche.Vertex, error) {
	if s.parseVertex != nil {
		return s.parseVertex(b)
	} else if s.cantParseVertex && s.t != nil {
		s.t.Fatal(errParseVertex)
	}
	return nil, errParseVertex
}

func (s *stateTest) BuildVertex(set ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
	if s.buildVertex != nil {
		return s.buildVertex(set, txs)
	}
	if s.cantBuildVertex && s.t != nil {
		s.t.Fatal(errBuildVertex)
	}
	return nil, errBuildVertex
}

func (s *stateTest) GetVertex(id ids.ID) (avalanche.Vertex, error) {
	if s.getVertex != nil {
		return s.getVertex(id)
	}
	if s.cantGetVertex && s.t != nil {
		s.t.Fatal(errGetVertex)
	}
	return nil, errGetVertex
}

func (s *stateTest) Edge() []ids.ID {
	if s.edge != nil {
		return s.edge()
	}
	if s.cantEdge && s.t != nil {
		s.t.Fatalf("Unexpectedly called Edge")
	}
	return nil
}

func (s *stateTest) SaveEdge(idList []ids.ID) {
	if s.saveEdge != nil {
		s.saveEdge(idList)
	} else if s.cantSaveEdge && s.t != nil {
		s.t.Fatalf("Unexpectedly called SaveEdge")
	}
}
