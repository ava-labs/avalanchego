// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

const reviewStatePending = "PENDING"

type DraftReviewEntryKind string

const (
	DraftReviewEntryKindNewThread   DraftReviewEntryKind = "new_thread"
	DraftReviewEntryKindThreadReply DraftReviewEntryKind = "thread_reply"
)

type User struct {
	Login string `json:"login"`
}

type DraftReviewEntry struct {
	Kind             DraftReviewEntryKind `json:"kind,omitempty"`
	ID               string               `json:"id,omitempty"`
	ThreadID         string               `json:"thread_id,omitempty"`
	ReplyToCommentID string               `json:"reply_to_comment_id,omitempty"`
	Path             string               `json:"path,omitempty"`
	Line             int                  `json:"line,omitempty"`
	Side             string               `json:"side,omitempty"`
	StartLine        int                  `json:"start_line,omitempty"`
	StartSide        string               `json:"start_side,omitempty"`
	Body             string               `json:"body"`
}

// Deprecated: kept as a compatibility alias while the command surface still uses
// the replace-comments name.
type DraftReviewComment = DraftReviewEntry

type ReviewComment struct {
	ID               string               `json:"id"`
	ThreadID         string               `json:"thread_id,omitempty"`
	Kind             DraftReviewEntryKind `json:"kind,omitempty"`
	ReplyToCommentID string               `json:"reply_to_comment_id,omitempty"`
	Path             string               `json:"path,omitempty"`
	Line             int                  `json:"line,omitempty"`
	Side             string               `json:"side,omitempty"`
	StartLine        int                  `json:"start_line,omitempty"`
	StartSide        string               `json:"start_side,omitempty"`
	Body             string               `json:"body"`
	User             User                 `json:"user"`
}

type Review struct {
	ID         string          `json:"id"`
	DatabaseID int64           `json:"database_id,omitempty"`
	State      string          `json:"state"`
	Body       string          `json:"body"`
	HTMLURL    string          `json:"html_url"`
	User       User            `json:"user"`
	Comments   []ReviewComment `json:"comments,omitempty"`
}
