// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"slices"
	"strconv"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
)

func FindPendingReviewForAuthor(reviews []Review, login string) (Review, bool) {
	for _, review := range reviews {
		if review.User.Login == login && review.State == reviewStatePending {
			return review, true
		}
	}
	return Review{}, false
}

func EnsureNoPendingReviewForAuthor(reviews []Review, login string) error {
	if review, found := FindPendingReviewForAuthor(reviews, login); found {
		return stacktrace.Errorf("refusing to create a new pending review because %s already has pending review %s", login, displayReviewID(review))
	}
	return nil
}

func normalizeReviewComments(comments []ReviewComment) []ReviewComment {
	normalized := append([]ReviewComment(nil), comments...)
	slices.SortFunc(normalized, compareReviewComments)
	return normalized
}

func compareReviewComments(left ReviewComment, right ReviewComment) int {
	return compareDraftReviewEntries(liveCommentToEntry(left), liveCommentToEntry(right))
}

func compareReviewCommentsForDelete(left ReviewComment, right ReviewComment) int {
	if value := compareReviewComments(left, right); value != 0 {
		return value
	}
	switch {
	case left.ID < right.ID:
		return -1
	case left.ID > right.ID:
		return 1
	default:
		return 0
	}
}

func liveCommentToEntry(comment ReviewComment) DraftReviewEntry {
	return DraftReviewEntry{
		Kind:             comment.Kind,
		ThreadID:         comment.ThreadID,
		ReplyToCommentID: comment.ReplyToCommentID,
		Path:             comment.Path,
		Line:             comment.Line,
		Side:             comment.Side,
		StartLine:        comment.StartLine,
		StartSide:        comment.StartSide,
		Body:             comment.Body,
	}
}

func entriesByKey(comments []ReviewComment) map[string][]ReviewComment {
	result := make(map[string][]ReviewComment)
	for _, comment := range normalizeReviewComments(comments) {
		key := draftReviewEntryKey(liveCommentToEntry(comment))
		result[key] = append(result[key], comment)
	}
	return result
}

func draftEntriesByKey(entries []DraftReviewEntry) map[string][]DraftReviewEntry {
	result := make(map[string][]DraftReviewEntry)
	for _, entry := range normalizeDraftReviewEntries(entries) {
		key := draftReviewEntryKey(entry)
		result[key] = append(result[key], entry)
	}
	return result
}

func draftReviewEntryKey(entry DraftReviewEntry) string {
	entry = normalizeDraftReviewEntry(entry)
	threadID := entry.ThreadID
	side := entry.Side
	startSide := entry.StartSide
	if entry.Kind != DraftReviewEntryKindThreadReply {
		threadID = ""
		side = ""
		startSide = ""
	}
	return string(entry.Kind) + "\x00" +
		threadID + "\x00" +
		entry.Path + "\x00" +
		side + "\x00" +
		startSide + "\x00" +
		entry.Body + "\x00" +
		strconv.Itoa(entry.Line) + "\x00" +
		strconv.Itoa(entry.StartLine)
}
