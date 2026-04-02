// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
)

const (
	reviewSideLeft  = "LEFT"
	reviewSideRight = "RIGHT"
)

func loadCommentsFile(path string) ([]DraftReviewEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()

	var entries []DraftReviewEntry
	if err := decoder.Decode(&entries); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return nil, stacktrace.Errorf("comments file %q must contain exactly one JSON value", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, stacktrace.Wrap(err)
	}

	for i := range entries {
		if entries[i].Kind == "" {
			entries[i].Kind = DraftReviewEntryKindNewThread
		}
		if err := validateDraftReviewEntry(entries[i]); err != nil {
			return nil, stacktrace.Errorf("comments[%d]: %w", i, err)
		}
	}
	return normalizeDraftReviewEntries(entries), nil
}

func validateDraftReviewEntry(entry DraftReviewEntry) error {
	if strings.TrimSpace(entry.Body) == "" {
		return stacktrace.New("body is required")
	}

	switch entry.Kind {
	case "", DraftReviewEntryKindNewThread:
		if strings.TrimSpace(entry.Path) == "" {
			return stacktrace.New("path is required")
		}
		if entry.Line <= 0 {
			return stacktrace.New("line must be a positive integer")
		}
		if entry.Side != reviewSideLeft && entry.Side != reviewSideRight {
			return stacktrace.Errorf("side must be LEFT or RIGHT, got %q", entry.Side)
		}
		if entry.StartLine < 0 {
			return stacktrace.New("start_line must be zero or a positive integer")
		}
		if entry.StartLine == 0 && entry.StartSide != "" {
			return stacktrace.New("start_side requires start_line")
		}
		if entry.StartLine > 0 && entry.StartSide != reviewSideLeft && entry.StartSide != reviewSideRight {
			return stacktrace.Errorf("start_side must be LEFT or RIGHT, got %q", entry.StartSide)
		}
		return nil

	case DraftReviewEntryKindThreadReply:
		if strings.TrimSpace(entry.ThreadID) == "" {
			return stacktrace.New("thread_id is required for thread_reply")
		}
		if entry.Path != "" || entry.Line != 0 || entry.Side != "" || entry.StartLine != 0 || entry.StartSide != "" {
			return stacktrace.New("thread_reply entries must not include file anchor fields")
		}
		return nil

	default:
		return stacktrace.Errorf("kind must be %q or %q, got %q", DraftReviewEntryKindNewThread, DraftReviewEntryKindThreadReply, entry.Kind)
	}
}

func normalizeDraftReviewEntries(entries []DraftReviewEntry) []DraftReviewEntry {
	normalized := append([]DraftReviewEntry(nil), entries...)
	for i := range normalized {
		if normalized[i].Kind == "" {
			normalized[i].Kind = DraftReviewEntryKindNewThread
		}
		normalized[i] = normalizeDraftReviewEntry(normalized[i])
	}
	slices.SortFunc(normalized, compareDraftReviewEntries)
	return normalized
}

func normalizeDraftReviewEntry(entry DraftReviewEntry) DraftReviewEntry {
	if entry.Kind != DraftReviewEntryKindThreadReply {
		return entry
	}
	entry.Path = ""
	entry.Line = 0
	entry.Side = ""
	entry.StartLine = 0
	entry.StartSide = ""
	return entry
}

func compareDraftReviewEntries(left DraftReviewEntry, right DraftReviewEntry) int {
	left = normalizeDraftReviewEntry(left)
	right = normalizeDraftReviewEntry(right)
	leftThreadID := left.ThreadID
	rightThreadID := right.ThreadID
	if left.Kind != DraftReviewEntryKindThreadReply {
		leftThreadID = ""
	}
	if right.Kind != DraftReviewEntryKindThreadReply {
		rightThreadID = ""
	}
	leftSide := left.Side
	rightSide := right.Side
	leftStartSide := left.StartSide
	rightStartSide := right.StartSide
	if left.Kind != DraftReviewEntryKindThreadReply && right.Kind != DraftReviewEntryKindThreadReply {
		if leftSide == "" || rightSide == "" {
			leftSide = ""
			rightSide = ""
		}
		if leftStartSide == "" || rightStartSide == "" {
			leftStartSide = ""
			rightStartSide = ""
		}
	}
	for _, pair := range [][2]string{
		{string(left.Kind), string(right.Kind)},
		{leftThreadID, rightThreadID},
		{left.Path, right.Path},
		{leftSide, rightSide},
		{leftStartSide, rightStartSide},
		{left.Body, right.Body},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return 1
		}
	}
	for _, pair := range [][2]int{
		{left.Line, right.Line},
		{left.StartLine, right.StartLine},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return 1
		}
	}
	return 0
}

func normalizeLiveReviewComments(comments []ReviewComment, author string) []DraftReviewEntry {
	normalized := make([]DraftReviewEntry, 0, len(comments))
	for _, comment := range comments {
		if author != "" && comment.User.Login != "" && comment.User.Login != author {
			continue
		}
		normalized = append(normalized, DraftReviewEntry{
			Kind:             comment.Kind,
			ThreadID:         comment.ThreadID,
			ReplyToCommentID: comment.ReplyToCommentID,
			Path:             comment.Path,
			Line:             comment.Line,
			Side:             comment.Side,
			StartLine:        comment.StartLine,
			StartSide:        comment.StartSide,
			Body:             comment.Body,
		})
	}
	return normalizeDraftReviewEntries(normalized)
}

func draftReviewEntriesEqual(left []DraftReviewEntry, right []DraftReviewEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if compareDraftReviewEntries(left[i], right[i]) != 0 {
			return false
		}
	}
	return true
}

func formatCommentsPathError(path string, err error) error {
	return stacktrace.Errorf("load comments from %q: %w", path, err)
}

func diffCommentsMessage(live []DraftReviewEntry, stored []DraftReviewEntry) string {
	return fmt.Sprintf("live=%d stored=%d", len(live), len(stored))
}
