// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// TODO: https://github.com/ava-labs/avalanchego/issues/3174
//go:build test || !test

package vertex

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

var (
	errParse = errors.New("unexpectedly called Parse")

	_ Parser = (*TestParser)(nil)
)

type TestParser struct {
	T            *testing.T
	CantParseVtx bool
	ParseVtxF    func(context.Context, []byte) (avalanche.Vertex, error)
}

func (p *TestParser) Default(cant bool) {
	p.CantParseVtx = cant
}

func (p *TestParser) ParseVtx(ctx context.Context, b []byte) (avalanche.Vertex, error) {
	if p.ParseVtxF != nil {
		return p.ParseVtxF(ctx, b)
	}
	if p.CantParseVtx && p.T != nil {
		require.FailNow(p.T, errParse.Error())
	}
	return nil, errParse
}
