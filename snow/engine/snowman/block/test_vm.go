// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	errBuildBlock   = errors.New("unexpectedly called BuildBlock")
	errParseBlock   = errors.New("unexpectedly called ParseBlock")
	errGetBlock     = errors.New("unexpectedly called GetBlock")
	errLastAccepted = errors.New("unexpectedly called LastAccepted")

	_ ChainVM = (*TestVM)(nil)
)

// TestVM is a ChainVM that is useful for testing.
type TestVM struct {
	common.TestVM

	CantBuildBlock,
	CantParseBlock,
	CantGetBlock,
	CantSetPreference,
	CantLastAccepted bool

	BuildBlockF    func(context.Context) (snowman.Block, error)
	ParseBlockF    func(context.Context, []byte) (snowman.Block, error)
	GetBlockF      func(context.Context, ids.ID) (snowman.Block, error)
	SetPreferenceF func(context.Context, ids.ID) error
	LastAcceptedF  func(context.Context) (ids.ID, error)
}

func (vm *TestVM) Default(cant bool) {
	vm.TestVM.Default(cant)

	vm.CantBuildBlock = cant
	vm.CantParseBlock = cant
	vm.CantGetBlock = cant
	vm.CantSetPreference = cant
	vm.CantLastAccepted = cant
}

func (vm *TestVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	if vm.BuildBlockF != nil {
		return vm.BuildBlockF(ctx)
	}
	if vm.CantBuildBlock && vm.T != nil {
		vm.T.Fatal(errBuildBlock)
	}
	return nil, errBuildBlock
}

func (vm *TestVM) ParseBlock(ctx context.Context, b []byte) (snowman.Block, error) {
	if vm.ParseBlockF != nil {
		return vm.ParseBlockF(ctx, b)
	}
	if vm.CantParseBlock && vm.T != nil {
		vm.T.Fatal(errParseBlock)
	}
	return nil, errParseBlock
}

func (vm *TestVM) GetBlock(ctx context.Context, id ids.ID) (snowman.Block, error) {
	if vm.GetBlockF != nil {
		return vm.GetBlockF(ctx, id)
	}
	if vm.CantGetBlock && vm.T != nil {
		vm.T.Fatal(errGetBlock)
	}
	return nil, errGetBlock
}

func (vm *TestVM) SetPreference(ctx context.Context, id ids.ID) error {
	if vm.SetPreferenceF != nil {
		return vm.SetPreferenceF(ctx, id)
	}
	if vm.CantSetPreference && vm.T != nil {
		vm.T.Fatalf("Unexpectedly called SetPreference")
	}
	return nil
}

func (vm *TestVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm.LastAcceptedF != nil {
		return vm.LastAcceptedF(ctx)
	}
	if vm.CantLastAccepted && vm.T != nil {
		vm.T.Fatal(errLastAccepted)
	}
	return ids.ID{}, errLastAccepted
}
