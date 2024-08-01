// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	errBuild = errors.New("unexpectedly called Build")

	_ vertex.Builder = (*TestBuilder)(nil)
)

type TestBuilder struct {
	T             *testing.T
	CantBuildVtx  bool
	BuildStopVtxF func(ctx context.Context, parentIDs []ids.ID) (avalanche.Vertex, error)
}

func (b *TestBuilder) Default(cant bool) {
	b.CantBuildVtx = cant
}

func (b *TestBuilder) BuildStopVtx(ctx context.Context, parentIDs []ids.ID) (avalanche.Vertex, error) {
	if b.BuildStopVtxF != nil {
		return b.BuildStopVtxF(ctx, parentIDs)
	}
	if b.CantBuildVtx && b.T != nil {
		require.FailNow(b.T, errBuild.Error())
	}
	return nil, errBuild
}
