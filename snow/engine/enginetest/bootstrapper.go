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

	errClear = errors.New("unexpectedly called Clear")
)

type Bootstrapper struct {
	Engine

	CantClear bool

	ClearF func(ctx context.Context) error
}

func (b *Bootstrapper) Default(cant bool) {
	b.Engine.Default(cant)

	b.CantClear = cant
}

func (b *Bootstrapper) Clear(ctx context.Context) error {
	if b.ClearF != nil {
		return b.ClearF(ctx)
	}
	if b.T != nil {
		require.False(b.T, b.CantClear, errClear)
	}
	return errClear
}
