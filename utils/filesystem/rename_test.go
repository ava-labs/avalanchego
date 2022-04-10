// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenameIfExists(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(os.TempDir(), "test-rename")
	if err != nil {
		t.Fatal(err)
	}
	a := f.Name()
	b := a + ".2"
	f.Close()

	// rename "a" to "b"
	renamed, err := RenameIfExists(a, b)
	assert.True(t, renamed)
	assert.NoError(t, err)

	// rename "b" to "a"
	renamed, err = RenameIfExists(b, a)
	assert.True(t, renamed)
	assert.NoError(t, err)

	// remove "a", but rename "a"->"b" should NOT error
	assert.NoError(t, os.RemoveAll(a))
	renamed, err = RenameIfExists(a, b)
	assert.False(t, renamed)
	assert.NoError(t, err)
}
