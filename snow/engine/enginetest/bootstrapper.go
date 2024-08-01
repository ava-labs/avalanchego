// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ common.BootstrapableEngine = (*BootstrapperTest)(nil)

	errClear = errors.New("unexpectedly called Clear")
)

type BootstrapperTest struct {
	EngineTest

	CantClear bool

	ClearF func(ctx context.Context) error
}

func (b *BootstrapperTest) Default(cant bool) {
	b.EngineTest.Default(cant)

	b.CantClear = cant
}

func (b *BootstrapperTest) Clear(ctx context.Context) error {
	if b.ClearF != nil {
		return b.ClearF(ctx)
	}
	if b.CantClear && b.T != nil {
		require.FailNow(b.T, errClear.Error())
	}
	return errClear
}
