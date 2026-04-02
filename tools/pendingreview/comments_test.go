// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

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

func TestNormalizeDraftReviewEntriesIgnoresReplyAnchorFields(t *testing.T) {
	t.Parallel()

	entries := []DraftReviewEntry{
		{
			Kind:             DraftReviewEntryKindThreadReply,
			ThreadID:         "thread-1",
			ReplyToCommentID: "comment-1",
			Path:             "a.go",
			Line:             7,
			Side:             reviewSideRight,
			StartLine:        6,
			StartSide:        reviewSideLeft,
			Body:             "reply",
		},
	}

	normalized := normalizeDraftReviewEntries(entries)
	require.Equal(t, []DraftReviewEntry{
		{
			Kind:             DraftReviewEntryKindThreadReply,
			ThreadID:         "thread-1",
			ReplyToCommentID: "comment-1",
			Body:             "reply",
		},
	}, normalized)
}

func TestDraftReviewEntriesEqualIgnoresReplyAnchorFields(t *testing.T) {
	t.Parallel()

	left := []DraftReviewEntry{{
		Kind:     DraftReviewEntryKindThreadReply,
		ThreadID: "thread-1",
		Body:     "reply",
	}}
	right := []DraftReviewEntry{{
		Kind:             DraftReviewEntryKindThreadReply,
		ThreadID:         "thread-1",
		ReplyToCommentID: "comment-1",
		Path:             "a.go",
		Line:             7,
		StartLine:        6,
		Body:             "reply",
	}}

	require.True(t, draftReviewEntriesEqual(left, right))
}
