// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

const reviewStatePending = "PENDING"

type User struct {
	Login string `json:"login"`
}

type DraftReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

type ReviewComment struct {
	ID                  int64  `json:"id"`
	PullRequestReviewID int64  `json:"pull_request_review_id,omitempty"`
	Path                string `json:"path"`
	Line                int    `json:"line"`
	Side                string `json:"side"`
	Body                string `json:"body"`
	User                User   `json:"user"`
}

type Review struct {
	ID       int64           `json:"id"`
	State    string          `json:"state"`
	Body     string          `json:"body"`
	HTMLURL  string          `json:"html_url"`
	User     User            `json:"user"`
	Comments []ReviewComment `json:"comments,omitempty"`
}
