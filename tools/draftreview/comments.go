package draftreview

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

func loadCommentsFile(path string) ([]DraftReviewComment, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()

	var comments []DraftReviewComment
	if err := decoder.Decode(&comments); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return nil, stacktrace.Errorf("comments file %q must contain exactly one JSON value", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, stacktrace.Wrap(err)
	}

	for i, comment := range comments {
		if err := validateDraftReviewComment(comment); err != nil {
			return nil, stacktrace.Errorf("comments[%d]: %w", i, err)
		}
	}
	return normalizeDraftReviewComments(comments), nil
}

func validateDraftReviewComment(comment DraftReviewComment) error {
	if strings.TrimSpace(comment.Path) == "" {
		return stacktrace.New("path is required")
	}
	if comment.Line <= 0 {
		return stacktrace.New("line must be a positive integer")
	}
	if comment.Side != "LEFT" && comment.Side != "RIGHT" {
		return stacktrace.Errorf("side must be LEFT or RIGHT, got %q", comment.Side)
	}
	if strings.TrimSpace(comment.Body) == "" {
		return stacktrace.New("body is required")
	}
	return nil
}

func normalizeDraftReviewComments(comments []DraftReviewComment) []DraftReviewComment {
	normalized := append([]DraftReviewComment(nil), comments...)
	slices.SortFunc(normalized, compareDraftReviewComments)
	return normalized
}

func compareDraftReviewComments(left DraftReviewComment, right DraftReviewComment) int {
	for _, pair := range [][2]string{
		{left.Path, right.Path},
		{left.Side, right.Side},
		{left.Body, right.Body},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return 1
		}
	}
	switch {
	case left.Line < right.Line:
		return -1
	case left.Line > right.Line:
		return 1
	default:
		return 0
	}
}

func normalizeLiveReviewComments(comments []ReviewComment, author string) []DraftReviewComment {
	normalized := make([]DraftReviewComment, 0, len(comments))
	for _, comment := range comments {
		if author != "" && comment.User.Login != "" && comment.User.Login != author {
			continue
		}
		normalized = append(normalized, DraftReviewComment{
			Path: comment.Path,
			Line: comment.Line,
			Side: comment.Side,
			Body: comment.Body,
		})
	}
	return normalizeDraftReviewComments(normalized)
}

func draftReviewCommentsEqual(left []DraftReviewComment, right []DraftReviewComment) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func formatCommentsPathError(path string, err error) error {
	return stacktrace.Errorf("load comments from %q: %w", path, err)
}

func marshalCommentsForCreate(comments []DraftReviewComment) []map[string]any {
	if len(comments) == 0 {
		return nil
	}

	encoded := make([]map[string]any, 0, len(comments))
	for _, comment := range comments {
		encoded = append(encoded, map[string]any{
			"path": comment.Path,
			"line": comment.Line,
			"side": comment.Side,
			"body": comment.Body,
		})
	}
	return encoded
}

func diffCommentsMessage(live []DraftReviewComment, stored []DraftReviewComment) string {
	return fmt.Sprintf("live=%d stored=%d", len(live), len(stored))
}
