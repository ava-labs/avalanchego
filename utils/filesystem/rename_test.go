// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameIfExists(t *testing.T) {
	require := require.New(t)

	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "test-rename")
	require.NoError(err)

	a := f.Name()
	b := a + ".2"

	require.NoError(f.Close())

	// rename "a" to "b"
	renamed, err := RenameIfExists(a, b)
	require.NoError(err)
	require.True(renamed)

	// rename "b" to "a"
	renamed, err = RenameIfExists(b, a)
	require.NoError(err)
	require.True(renamed)

	// remove "a", but rename "a"->"b" should NOT error
	require.NoError(os.RemoveAll(a))
	renamed, err = RenameIfExists(a, b)
	require.NoError(err)
	require.False(renamed)
}
