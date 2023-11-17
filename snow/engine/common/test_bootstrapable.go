// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	_ Bootstrapable = (*BootstrapableTest)(nil)

	errForceAccepted = errors.New("unexpectedly called ForceAccepted")
	errClear         = errors.New("unexpectedly called Clear")
)

// BootstrapableTest is a test engine that supports bootstrapping
type BootstrapableTest struct {
	T *testing.T

	CantClear bool

	ClearF func(ctx context.Context) error
}

// Default sets the default on call handling
func (b *BootstrapableTest) Default(cant bool) {
	b.CantClear = cant
}

func (b *BootstrapableTest) Clear(ctx context.Context) error {
	if b.ClearF != nil {
		return b.ClearF(ctx)
	}
	if b.CantClear && b.T != nil {
		require.FailNow(b.T, errClear.Error())
	}
	return errClear
}
