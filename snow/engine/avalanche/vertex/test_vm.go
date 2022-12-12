// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	errPending = errors.New("unexpectedly called Pending")

	_ DAGVM = (*TestVM)(nil)
)

type TestVM struct {
	common.TestVM

	CantPendingTxs, CantParse, CantGet bool

	PendingTxsF func(context.Context) []snowstorm.Tx
	ParseTxF    func(context.Context, []byte) (snowstorm.Tx, error)
	GetTxF      func(context.Context, ids.ID) (snowstorm.Tx, error)
}

func (vm *TestVM) Default(cant bool) {
	vm.TestVM.Default(cant)

	vm.CantPendingTxs = cant
	vm.CantParse = cant
	vm.CantGet = cant
}

func (vm *TestVM) PendingTxs(ctx context.Context) []snowstorm.Tx {
	if vm.PendingTxsF != nil {
		return vm.PendingTxsF(ctx)
	}
	if vm.CantPendingTxs && vm.T != nil {
		vm.T.Fatal(errPending)
	}
	return nil
}

func (vm *TestVM) ParseTx(ctx context.Context, b []byte) (snowstorm.Tx, error) {
	if vm.ParseTxF != nil {
		return vm.ParseTxF(ctx, b)
	}
	if vm.CantParse && vm.T != nil {
		vm.T.Fatal(errParse)
	}
	return nil, errParse
}

func (vm *TestVM) GetTx(ctx context.Context, txID ids.ID) (snowstorm.Tx, error) {
	if vm.GetTxF != nil {
		return vm.GetTxF(ctx, txID)
	}
	if vm.CantGet && vm.T != nil {
		vm.T.Fatal(errGet)
	}
	return nil, errGet
}
