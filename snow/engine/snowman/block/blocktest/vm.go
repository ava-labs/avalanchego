// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	errBuildBlock         = errors.New("unexpectedly called BuildBlock")
	errParseBlock         = errors.New("unexpectedly called ParseBlock")
	errGetBlock           = errors.New("unexpectedly called GetBlock")
	errLastAccepted       = errors.New("unexpectedly called LastAccepted")
	errGetBlockIDAtHeight = errors.New("unexpectedly called GetBlockIDAtHeight")

	_ block.ChainVM = (*VM)(nil)
)

// VM is a ChainVM that is useful for testing.
type VM struct {
	enginetest.VM

	CantBuildBlock,
	CantParseBlock,
	CantGetBlock,
	CantSetPreference,
	CantLastAccepted,
	CantGetBlockIDAtHeight bool

	BuildBlockF         func(context.Context) (snowman.Block, error)
	ParseBlockF         func(context.Context, []byte) (snowman.Block, error)
	GetBlockF           func(context.Context, ids.ID) (snowman.Block, error)
	SetPreferenceF      func(context.Context, ids.ID) error
	LastAcceptedF       func(context.Context) (ids.ID, error)
	GetBlockIDAtHeightF func(ctx context.Context, height uint64) (ids.ID, error)
}

func (vm *VM) Default(cant bool) {
	vm.VM.Default(cant)

	vm.CantBuildBlock = cant
	vm.CantParseBlock = cant
	vm.CantGetBlock = cant
	vm.CantSetPreference = cant
	vm.CantLastAccepted = cant
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	if vm.BuildBlockF != nil {
		return vm.BuildBlockF(ctx)
	}
	if vm.CantBuildBlock && vm.T != nil {
		require.FailNow(vm.T, errBuildBlock.Error())
	}
	return nil, errBuildBlock
}

func (vm *VM) ParseBlock(ctx context.Context, b []byte) (snowman.Block, error) {
	if vm.ParseBlockF != nil {
		return vm.ParseBlockF(ctx, b)
	}
	if vm.CantParseBlock && vm.T != nil {
		require.FailNow(vm.T, errParseBlock.Error())
	}
	return nil, errParseBlock
}

func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (snowman.Block, error) {
	if vm.GetBlockF != nil {
		return vm.GetBlockF(ctx, id)
	}
	if vm.CantGetBlock && vm.T != nil {
		require.FailNow(vm.T, errGetBlock.Error())
	}
	return nil, errGetBlock
}

func (vm *VM) SetPreference(ctx context.Context, id ids.ID) error {
	if vm.SetPreferenceF != nil {
		return vm.SetPreferenceF(ctx, id)
	}
	if vm.CantSetPreference && vm.T != nil {
		require.FailNow(vm.T, "Unexpectedly called SetPreference")
	}
	return nil
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm.LastAcceptedF != nil {
		return vm.LastAcceptedF(ctx)
	}
	if vm.CantLastAccepted && vm.T != nil {
		require.FailNow(vm.T, errLastAccepted.Error())
	}
	return ids.Empty, errLastAccepted
}

func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm.GetBlockIDAtHeightF != nil {
		return vm.GetBlockIDAtHeightF(ctx, height)
	}
	if vm.CantGetBlockIDAtHeight && vm.T != nil {
		require.FailNow(vm.T, errGetAncestor.Error())
	}
	return ids.Empty, errGetBlockIDAtHeight
}
