// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameIfExists(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(os.TempDir(), "test-rename")
	require.NoError(t, err)

	a := f.Name()
	b := a + ".2"

	require.NoError(t, f.Close())

	// rename "a" to "b"
	renamed, err := RenameIfExists(a, b)
	require.True(t, renamed)
	require.NoError(t, err)

	// rename "b" to "a"
	renamed, err = RenameIfExists(b, a)
	require.True(t, renamed)
	require.NoError(t, err)

	// remove "a", but rename "a"->"b" should NOT error
	require.NoError(t, os.RemoveAll(a))
	renamed, err = RenameIfExists(a, b)
	require.False(t, renamed)
	require.NoError(t, err)
}
