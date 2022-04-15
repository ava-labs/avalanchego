// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"errors"
	"testing"

	"github.com/chain4travel/caminogo/snow/consensus/avalanche"
)

var (
	errParse = errors.New("unexpectedly called Parse")

	_ Parser = &TestParser{}
)

type TestParser struct {
	T            *testing.T
	CantParseVtx bool
	ParseVtxF    func([]byte) (avalanche.Vertex, error)
}

func (p *TestParser) Default(cant bool) { p.CantParseVtx = cant }

func (p *TestParser) ParseVtx(b []byte) (avalanche.Vertex, error) {
	if p.ParseVtxF != nil {
		return p.ParseVtxF(b)
	}
	if p.CantParseVtx && p.T != nil {
		p.T.Fatal(errParse)
	}
	return nil, errParse
}
