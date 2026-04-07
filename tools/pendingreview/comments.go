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
	"strconv"
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
	live = normalizeDraftReviewEntries(live)
	stored = normalizeDraftReviewEntries(stored)

	liveBySelector := draftEntriesBySelector(live)
	storedBySelector := draftEntriesBySelector(stored)

	selectors := make([]string, 0, len(liveBySelector)+len(storedBySelector))
	for selector := range liveBySelector {
		selectors = append(selectors, selector)
	}
	for selector := range storedBySelector {
		if _, ok := liveBySelector[selector]; !ok {
			selectors = append(selectors, selector)
		}
	}
	slices.Sort(selectors)

	messages := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		liveEntries := liveBySelector[selector]
		storedEntries := storedBySelector[selector]
		switch {
		case len(liveEntries) == 0:
			messages = append(messages, "stored-only "+describeDraftReviewEntry(storedEntries[0]))
		case len(storedEntries) == 0:
			messages = append(messages, "live-only "+describeDraftReviewEntry(liveEntries[0]))
		default:
			liveBodies := entryBodies(liveEntries)
			storedBodies := entryBodies(storedEntries)
			if !slices.Equal(liveBodies, storedBodies) {
				messages = append(messages, fmt.Sprintf(
					"body differs at %s (live=%q stored=%q)",
					describeDraftReviewEntrySelector(liveEntries[0]),
					strings.Join(liveBodies, ", "),
					strings.Join(storedBodies, ", "),
				))
			}
			if len(liveEntries) != len(storedEntries) {
				messages = append(messages, fmt.Sprintf(
					"count differs at %s (live=%d stored=%d)",
					describeDraftReviewEntrySelector(liveEntries[0]),
					len(liveEntries),
					len(storedEntries),
				))
			}
		}
	}
	if len(messages) == 0 {
		return fmt.Sprintf("live=%d stored=%d", len(live), len(stored))
	}
	return strings.Join(messages, "; ")
}

type upsertSelector struct {
	CommentID string
	Kind      DraftReviewEntryKind
	ThreadID  string
	Path      string
	Line      int
	Side      string
	StartLine int
	StartSide string
}

func buildUpsertDesiredEntries(comments []ReviewComment, author string, selector upsertSelector, body string, storedEntries []DraftReviewEntry) ([]DraftReviewEntry, []DraftReviewEntry, error) {
	managedComments := make([]ReviewComment, 0, len(comments))
	for _, comment := range comments {
		if author != "" && comment.User.Login != "" && comment.User.Login != author {
			continue
		}
		managedComments = append(managedComments, comment)
	}

	liveEntries := normalizeLiveReviewComments(managedComments, "")
	desiredEntries := make([]DraftReviewEntry, 0, len(managedComments)+1)
	replaced := false
	for _, comment := range managedComments {
		if selector.CommentID != "" && comment.ID == selector.CommentID {
			updated := liveCommentToEntry(comment)
			if updated.Kind == DraftReviewEntryKindNewThread {
				storedAnchor, err := fillStoredAnchorForCommentIDUpdate(storedEntries, updated)
				if err != nil {
					return nil, nil, err
				}
				updated.Path = storedAnchor.Path
				updated.Line = storedAnchor.Line
				updated.Side = storedAnchor.Side
				updated.StartLine = storedAnchor.StartLine
				updated.StartSide = storedAnchor.StartSide
			}
			updated.Body = body
			desiredEntries = append(desiredEntries, updated)
			replaced = true
			continue
		}

		entry := liveCommentToEntry(comment)
		if selector.CommentID == "" && selector.matchesEntry(entry) {
			if replaced {
				return nil, nil, stacktrace.Errorf("multiple managed comments match %s; use --comment-id or replace-comments", describeDraftReviewEntrySelector(entry))
			}
			entry = selector.entryWithBody(body)
			replaced = true
		}
		desiredEntries = append(desiredEntries, entry)
	}

	if selector.CommentID != "" && !replaced {
		return nil, nil, stacktrace.Errorf("no managed pending review comment with id %q", selector.CommentID)
	}
	if selector.CommentID == "" && !replaced {
		desiredEntries = append(desiredEntries, DraftReviewEntry{
			Kind:      DraftReviewEntryKindNewThread,
			Path:      selector.Path,
			Line:      selector.Line,
			Side:      selector.Side,
			StartLine: selector.StartLine,
			StartSide: selector.StartSide,
			Body:      body,
		})
	}
	return normalizeDraftReviewEntries(desiredEntries), liveEntries, nil
}

func fillStoredAnchorForCommentIDUpdate(storedEntries []DraftReviewEntry, liveEntry DraftReviewEntry) (DraftReviewEntry, error) {
	if liveEntry.Kind != DraftReviewEntryKindNewThread || liveEntry.Side != "" {
		return liveEntry, nil
	}

	var match *DraftReviewEntry
	for _, entry := range normalizeDraftReviewEntries(storedEntries) {
		if entry.Kind != DraftReviewEntryKindNewThread {
			continue
		}
		if entry.Path != liveEntry.Path || entry.Line != liveEntry.Line || entry.StartLine != liveEntry.StartLine {
			continue
		}
		if match != nil {
			return DraftReviewEntry{}, stacktrace.Errorf("multiple stored comments match new_thread %s:%d; use anchor targeting or replace-comments", liveEntry.Path, liveEntry.Line)
		}
		entryCopy := entry
		match = &entryCopy
	}
	if match == nil {
		return DraftReviewEntry{}, stacktrace.Errorf("stored state does not include a unique anchor for comment-id update on %s:%d; use anchor targeting or replace-comments", liveEntry.Path, liveEntry.Line)
	}
	return *match, nil
}

func removeTargetedEntriesForComparison(stored []DraftReviewEntry, live []DraftReviewEntry, selector upsertSelector) ([]DraftReviewEntry, []DraftReviewEntry, error) {
	storedRemaining, err := removeMatchingEntry(stored, selector)
	if err != nil {
		return nil, nil, err
	}
	liveRemaining, err := removeMatchingEntry(live, selector)
	if err != nil {
		return nil, nil, err
	}
	return storedRemaining, liveRemaining, nil
}

func removeMatchingEntry(entries []DraftReviewEntry, selector upsertSelector) ([]DraftReviewEntry, error) {
	remaining := make([]DraftReviewEntry, 0, len(entries))
	removed := false
	for _, entry := range entries {
		if selector.matchesEntry(entry) {
			if removed {
				return nil, stacktrace.Errorf("multiple managed comments match %s; use --comment-id or replace-comments", describeDraftReviewEntrySelector(entry))
			}
			removed = true
			continue
		}
		remaining = append(remaining, entry)
	}
	return normalizeDraftReviewEntries(remaining), nil
}

func (selector upsertSelector) matchesEntry(entry DraftReviewEntry) bool {
	entry = normalizeDraftReviewEntry(entry)
	if selector.CommentID != "" {
		return false
	}
	threadID := selector.ThreadID
	if selector.Kind != DraftReviewEntryKindThreadReply {
		threadID = ""
	}
	entryThreadID := entry.ThreadID
	if entry.Kind != DraftReviewEntryKindThreadReply {
		entryThreadID = ""
	}
	return entry.Kind == selector.Kind &&
		entryThreadID == threadID &&
		entry.Path == selector.Path &&
		entry.Line == selector.Line &&
		sidesCompatible(entry.Side, selector.Side) &&
		entry.StartLine == selector.StartLine &&
		sidesCompatible(entry.StartSide, selector.StartSide)
}

func (selector upsertSelector) entryWithBody(body string) DraftReviewEntry {
	return DraftReviewEntry{
		Kind:      selector.Kind,
		ThreadID:  selector.ThreadID,
		Path:      selector.Path,
		Line:      selector.Line,
		Side:      selector.Side,
		StartLine: selector.StartLine,
		StartSide: selector.StartSide,
		Body:      body,
	}
}

func sidesCompatible(entrySide string, selectorSide string) bool {
	return entrySide == "" || selectorSide == "" || entrySide == selectorSide
}

func draftEntriesBySelector(entries []DraftReviewEntry) map[string][]DraftReviewEntry {
	result := make(map[string][]DraftReviewEntry)
	for _, entry := range normalizeDraftReviewEntries(entries) {
		key := draftReviewEntrySelectorKey(entry)
		result[key] = append(result[key], entry)
	}
	return result
}

func draftReviewEntrySelectorKey(entry DraftReviewEntry) string {
	entry = normalizeDraftReviewEntry(entry)
	return string(entry.Kind) + "\x00" +
		entry.ThreadID + "\x00" +
		entry.Path + "\x00" +
		entry.Side + "\x00" +
		entry.StartSide + "\x00" +
		strconv.Itoa(entry.Line) + "\x00" +
		strconv.Itoa(entry.StartLine)
}

func describeDraftReviewEntry(entry DraftReviewEntry) string {
	return fmt.Sprintf("%s %q", describeDraftReviewEntrySelector(entry), entry.Body)
}

func describeDraftReviewEntrySelector(entry DraftReviewEntry) string {
	entry = normalizeDraftReviewEntry(entry)
	if entry.Kind == DraftReviewEntryKindThreadReply {
		return "thread_reply thread=" + entry.ThreadID
	}
	location := fmt.Sprintf("%s:%d", entry.Path, entry.Line)
	if entry.Side != "" {
		location += " " + entry.Side
	}
	if entry.StartLine > 0 {
		location = fmt.Sprintf("%s-%d %s->%s", entry.Path, entry.StartLine, entry.StartSide, location)
	}
	return "new_thread " + location
}

func entryBodies(entries []DraftReviewEntry) []string {
	bodies := make([]string, 0, len(entries))
	for _, entry := range entries {
		bodies = append(bodies, entry.Body)
	}
	slices.Sort(bodies)
	return bodies
}
