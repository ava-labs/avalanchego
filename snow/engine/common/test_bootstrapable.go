// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Bootstrapable = (*BootstrapableTest)(nil)

	errForceAccepted = errors.New("unexpectedly called ForceAccepted")
	errClear         = errors.New("unexpectedly called Clear")
)

// BootstrapableTest is a test engine that supports bootstrapping
type BootstrapableTest struct {
	T *testing.T

	CantForceAccepted, CantClear bool

	ClearF         func() error
	ForceAcceptedF func(ctx context.Context, acceptedContainerIDs []ids.ID) error
}

// Default sets the default on call handling
func (b *BootstrapableTest) Default(cant bool) {
	b.CantForceAccepted = cant
}

func (b *BootstrapableTest) Clear() error {
	if b.ClearF != nil {
		return b.ClearF()
	}
	if b.CantClear && b.T != nil {
		require.FailNow(b.T, errClear.Error())
	}
	return errClear
}

func (b *BootstrapableTest) ForceAccepted(ctx context.Context, containerIDs []ids.ID) error {
	if b.ForceAcceptedF != nil {
		return b.ForceAcceptedF(ctx, containerIDs)
	}
	if b.CantForceAccepted && b.T != nil {
		require.FailNow(b.T, errForceAccepted.Error())
	}
	return errForceAccepted
}
