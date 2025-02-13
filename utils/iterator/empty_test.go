// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmpty(t *testing.T) {
	var (
		require = require.New(t)
		empty   = Empty[*int]{}
	)

	require.False(empty.Next())

	empty.Release()

	require.False(empty.Next())
	require.Nil(empty.Value())
}
