// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ Engine = (*EngineTest)(nil)

	errGetVtx = errors.New("unexpectedly called GetVtx")
)

// EngineTest is a test engine
type EngineTest struct {
	common.EngineTest

	CantGetVtx bool
	GetVtxF    func(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error)
}

func (e *EngineTest) Default(cant bool) {
	e.EngineTest.Default(cant)
	e.CantGetVtx = false
}

func (e *EngineTest) GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
	if e.GetVtxF != nil {
		return e.GetVtxF(ctx, vtxID)
	}
	if e.CantGetVtx && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetVtx")
	}
	return nil, errGetVtx
}
