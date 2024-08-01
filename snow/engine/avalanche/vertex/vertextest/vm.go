// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertextest

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
)

var (
	errLinearize = errors.New("unexpectedly called Linearize")

	_ vertex.LinearizableVM = (*TestVM)(nil)
)

type TestVM struct {
	blocktest.TestVM

	CantLinearize, CantParse bool

	LinearizeF func(context.Context, ids.ID) error
	ParseTxF   func(context.Context, []byte) (snowstorm.Tx, error)
}

func (vm *TestVM) Default(cant bool) {
	vm.TestVM.Default(cant)

	vm.CantParse = cant
}

func (vm *TestVM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	if vm.LinearizeF != nil {
		return vm.LinearizeF(ctx, stopVertexID)
	}
	if vm.CantLinearize && vm.T != nil {
		require.FailNow(vm.T, errLinearize.Error())
	}
	return errLinearize
}

func (vm *TestVM) ParseTx(ctx context.Context, b []byte) (snowstorm.Tx, error) {
	if vm.ParseTxF != nil {
		return vm.ParseTxF(ctx, b)
	}
	if vm.CantParse && vm.T != nil {
		require.FailNow(vm.T, errParse.Error())
	}
	return nil, errParse
}
