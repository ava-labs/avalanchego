// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

var (
	errParseVertex = errors.New("unexpectedly called ParseVertex")
)

type TestParser struct {
	T               *testing.T
	CantParseVertex bool
	ParseVertexF    func([]byte) (avalanche.Vertex, error)
}

func (p *TestParser) Default(cant bool) { p.CantParseVertex = cant }

func (p *TestParser) ParseVertex(b []byte) (avalanche.Vertex, error) {
	if p.ParseVertexF != nil {
		return p.ParseVertexF(b)
	}
	if p.CantParseVertex && p.T != nil {
		p.T.Fatal(errParseVertex)
	}
	return nil, errParseVertex
}
