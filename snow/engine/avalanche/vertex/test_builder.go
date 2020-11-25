// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
)

var (
	errBuildVertex = errors.New("unexpectedly called BuildVertex")
)

type TestBuilder struct {
	T               *testing.T
	CantBuildVertex bool
	BuildVertexF    func([]ids.ID, []snowstorm.Tx) (avalanche.Vertex, error)
}

func (b *TestBuilder) Default(cant bool) { b.CantBuildVertex = cant }

func (b *TestBuilder) BuildVertex(set []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
	if b.BuildVertexF != nil {
		return b.BuildVertexF(set, txs)
	}
	if b.CantBuildVertex && b.T != nil {
		b.T.Fatal(errBuildVertex)
	}
	return nil, errBuildVertex
}
