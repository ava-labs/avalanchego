// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomic(t *testing.T) {
	require := require.New(t)

	var a Atomic[bool]
	require.Zero(a.Get())

	a.Set(false)
	require.False(a.Get())

	a.Set(true)
	require.True(a.Get())

	a.Set(false)
	require.False(a.Get())
}
