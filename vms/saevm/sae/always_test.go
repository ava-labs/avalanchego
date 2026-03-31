// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
)

func TestSinceGenesisBeforeInit(t *testing.T) {
	ctx := t.Context()
	sut := NewSinceGenesis(hookstest.NewStub(0), Config{})
	t.Run(fmt.Sprintf("%T.Version", sut), func(t *testing.T) {
		got, err := sut.Version(ctx)
		require.NoError(t, err)
		require.Equal(t, version.Current.String(), got)
	})
	require.NoErrorf(t, sut.Shutdown(t.Context()), "%T.Shutdown()", sut)
}
