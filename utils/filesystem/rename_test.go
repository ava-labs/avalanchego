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

	err = f.Close()
	require.NoError(t, err)

	// rename "a" to "b"
	renamed, err := RenameIfExists(a, b)
	require.True(t, renamed)
	require.NoError(t, err)

	// rename "b" to "a"
	renamed, err = RenameIfExists(b, a)
	require.True(t, renamed)
	require.NoError(t, err)

	// remove "a", but rename "a"->"b" should NOT error
	err = os.RemoveAll(a)
	require.NoError(t, err)
	renamed, err = RenameIfExists(a, b)
	require.False(t, renamed)
	require.NoError(t, err)
}
