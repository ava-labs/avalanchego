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

package avalanche

import (
	"errors"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/consensus/avalanche"
	"github.com/chain4travel/caminogo/snow/engine/common"
)

var (
	_ Engine = &EngineTest{}

	errGetVtx = errors.New("unexpectedly called GetVtx")
)

// EngineTest is a test engine
type EngineTest struct {
	common.EngineTest

	CantGetVtx bool
	GetVtxF    func(vtxID ids.ID) (avalanche.Vertex, error)
}

func (e *EngineTest) Default(cant bool) {
	e.EngineTest.Default(cant)
	e.CantGetVtx = false
}

func (e *EngineTest) GetVtx(vtxID ids.ID) (avalanche.Vertex, error) {
	if e.GetVtxF != nil {
		return e.GetVtxF(vtxID)
	}
	if e.CantGetVtx && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetVtx")
	}
	return nil, errGetVtx
}
