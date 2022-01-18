// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	errBuild = errors.New("unexpectedly called Build")

	_ Builder = &TestBuilder{}
)

type TestBuilder struct {
	T             *testing.T
	CantBuildVtx  bool
	BuildVtxF     func(parentIDs []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error)
	BuildStopVtxF func(parentIDs []ids.ID) (avalanche.Vertex, error)
}

func (b *TestBuilder) Default(cant bool) { b.CantBuildVtx = cant }

func (b *TestBuilder) BuildVtx(parentIDs []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
	if b.BuildVtxF != nil {
		return b.BuildVtxF(parentIDs, txs)
	}
	if b.CantBuildVtx && b.T != nil {
		b.T.Fatal(errBuild)
	}
	return nil, errBuild
}

func (b *TestBuilder) BuildStopVtx(parentIDs []ids.ID) (avalanche.Vertex, error) {
	if b.BuildStopVtxF != nil {
		return b.BuildStopVtxF(parentIDs)
	}
	if b.CantBuildVtx && b.T != nil {
		b.T.Fatal(errBuild)
	}
	return nil, errBuild
}
