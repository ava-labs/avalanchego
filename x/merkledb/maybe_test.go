// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

func TestMaybeClone(t *testing.T) {
	// Case: Value is maybe
	{
		val := []byte{1, 2, 3}
		originalVal := slices.Clone(val)
		m := Some(val)
		mClone := Clone(m)
		m.value[0] = 0
		require.NotEqual(t, mClone.value, m.value)
		require.Equal(t, originalVal, mClone.value)
	}

	// Case: Value is nothing
	{
		m := Nothing[[]byte]()
		mClone := Clone(m)
		require.True(t, mClone.IsNothing())
	}
}
