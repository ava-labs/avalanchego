// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ common.BootstrapableEngine = (*Bootstrapper)(nil)

	errClear       = errors.New("unexpectedly called Clear")
	errHasProgress = errors.New("unexpectedly called HasProgress")
)

type Bootstrapper struct {
	Engine

	CantClear       bool
	CantHasProgress bool

	ClearF       func(ctx context.Context) error
	HasProgressF func(ctx context.Context) (bool, error)
}

func (b *Bootstrapper) Default(cant bool) {
	b.Engine.Default(cant)

	b.CantClear = cant
	b.CantHasProgress = cant
}

func (b *Bootstrapper) Clear(ctx context.Context) error {
	if b.ClearF != nil {
		return b.ClearF(ctx)
	}
	if b.CantClear && b.T != nil {
		require.FailNow(b.T, errClear.Error())
	}
	return errClear
}

func (b *Bootstrapper) HasProgress(ctx context.Context) (bool, error) {
	if b.HasProgressF != nil {
		return b.HasProgressF(ctx)
	}
	if b.CantHasProgress && b.T != nil {
		require.FailNow(b.T, errHasProgress.Error())
	}
	return false, errHasProgress
}
