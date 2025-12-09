// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"testing"
	"github.com/stretchr/testify/require"
)

func TestFoo(t *testing.T) {
	n := New(t)

	// ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	// defer cancel()

	// require.NoError(t, n.Start(ctx))
	require.NoError(t, n.Start(t.Context()))
}
