// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package draftreview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCommentsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "comments.json")
	content := `[
  {"path":"b.go","line":2,"side":"RIGHT","body":"second"},
  {"path":"a.go","line":1,"side":"RIGHT","body":"first"}
]`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	comments, err := loadCommentsFile(path)
	require.NoError(t, err)
	require.Len(t, comments, 2)
	require.Equal(t, "a.go", comments[0].Path)
}

func TestLoadCommentsFileRejectsUnknownField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "comments.json")
	content := `[{"path":"a.go","line":1,"side":"RIGHT","body":"first","position":3}]`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := loadCommentsFile(path)
	require.EqualError(t, err, `json: unknown field "position"`)
}
