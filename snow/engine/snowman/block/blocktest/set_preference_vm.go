// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	errSetPreferenceWithContext = errors.New("unexpectedly called SetPreferenceWithContext")

	_ block.SetPreferenceWithContextChainVM = (*SetPreferenceVM)(nil)
)

type SetPreferenceVM struct {
	T *testing.T

	CantSetPreferenceWithContext bool
	SetPreferenceWithContextF    func(context.Context, ids.ID, *block.Context) error
}

func (vm *SetPreferenceVM) Default(cant bool) {
	vm.CantSetPreferenceWithContext = cant
}

func (vm *SetPreferenceVM) SetPreferenceWithContext(ctx context.Context, id ids.ID, blockCtx *block.Context) error {
	if vm.SetPreferenceWithContextF != nil {
		return vm.SetPreferenceWithContextF(ctx, id, blockCtx)
	}
	if vm.CantSetPreferenceWithContext && vm.T != nil {
		require.FailNow(vm.T, errSetPreferenceWithContext.Error())
	}
	return errSetPreferenceWithContext
}
