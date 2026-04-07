// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type staticTokenProvider struct {
	token string
}

func (p staticTokenProvider) Token(context.Context, string) (string, error) {
	return p.token, nil
}

func TestRunCreateLogsStructuredOperations(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/graphql", r.URL.Path)
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "query PullRequestContext"):
			_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"maru"},"repository":{"pullRequest":{"id":"pr-1","reviewThreads":{"nodes":[]},"reviews":{"nodes":[{"id":"review-0","databaseId":111,"state":"PENDING","body":"existing","url":"https://example.invalid/review/111","author":{"login":"maru"},"comments":{"nodes":[]}}]}}}}}`)
		case strings.Contains(payload.Query, "mutation CreatePendingReview"):
			_, _ = io.WriteString(w, `{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"review-123","databaseId":123,"state":"PENDING","body":"test","url":"https://example.invalid/review/123","author":{"login":"maru"}}}}}`)
		default:
			require.FailNowf(t, "unexpected query", "query: %s", payload.Query)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL
	app.log = logging.NewLogger(
		"pendingreview",
		logging.NewWrappedCore(logging.Debug, nopWriteCloser{Writer: &stderr}, logging.JSON.ConsoleEncoder()),
	)

	stateDir := t.TempDir()
	require.NoError(t, app.Run(t.Context(), []string{
		"create",
		"--repo", "ava-labs/avalanchego",
		"--pr", "5168",
		"--body", "test",
		"--config-dir", t.TempDir(),
		"--state-dir", stateDir,
	}))

	logOutput := stderr.String()
	for _, expected := range []string{
		`"msg":"running command"`,
		`"command":"create"`,
		`"msg":"creating pending review"`,
		`"msg":"sending GitHub API request"`,
		`"msg":"received GitHub API response"`,
		`"msg":"saving review state"`,
		`"msg":"saved review state"`,
		`"msg":"created pending review"`,
		`"repo":"ava-labs/avalanchego"`,
		`"prNumber":5168`,
		`"reviewID":"review-123"`,
	} {
		require.Contains(t, logOutput, expected)
	}
}

func TestDefaultLoggerDoesNotEmitSuccessLogs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "query Viewer"):
			_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"maru"}}}`)
		case strings.Contains(payload.Query, "query PullRequestContext"):
			_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"maru"},"repository":{"pullRequest":{"id":"pr-1","reviewThreads":{"nodes":[]},"reviews":{"nodes":[{"id":"review-123","databaseId":123,"state":"PENDING","body":"body","url":"https://example.invalid/review/123","author":{"login":"maru"},"comments":{"nodes":[]}}]}}}}}`)
		default:
			require.FailNowf(t, "unexpected query", "query: %s", payload.Query)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"get",
		"--repo", "ava-labs/avalanchego",
		"--pr", "5168",
		"--config-dir", t.TempDir(),
		"--state-dir", t.TempDir(),
	}))

	require.NotEmpty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunUpdateBodyLogsStateComparison(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:                 "ava-labs/avalanchego",
		PRNumber:             5168,
		UserLogin:            "maru",
		ReviewID:             "review-123",
		LastPublishedBody:    "live body",
		LastPublishedEntries: []DraftReviewEntry{{Kind: DraftReviewEntryKindNewThread, Path: "a.go", Line: 7, Side: reviewSideRight, Body: "stored comment"}},
	}))

	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("live body", nil)
		case strings.Contains(query, "mutation UpdatePendingReviewBody"):
			require.Equal(t, "review-123", variables["reviewID"])
			require.Equal(t, "updated body", variables["body"])
			return map[string]any{
				"updatePullRequestReview": map[string]any{
					"pullRequestReview": map[string]any{
						"id":         "review-123",
						"databaseId": 123,
						"state":      reviewStatePending,
						"body":       "updated body",
						"url":        "https://example.invalid/review/123",
						"author":     map[string]any{"login": "maru"},
					},
				},
			}
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL
	app.log = logging.NewLogger(
		"pendingreview",
		logging.NewWrappedCore(logging.Debug, nopWriteCloser{Writer: &stderr}, logging.JSON.ConsoleEncoder()),
	)

	require.NoError(t, app.Run(t.Context(), []string{
		"update-body",
		"--pr", "5168",
		"--body", "updated body",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	logOutput := stderr.String()
	for _, expected := range []string{
		`"msg":"fetched live pending review for body update"`,
		`"msg":"loaded stored pending review state for body update"`,
		`"msg":"applying pending review body update"`,
		`"reviewID":"review-123"`,
		`"liveBodyLength":9`,
		`"storedBodyLength":9`,
		`"desiredBodyLength":12`,
		`"reviewIDMatches":true`,
		`"bodyMatches":true`,
		`"preservedEntryCount":1`,
	} {
		require.Contains(t, logOutput, expected)
	}
}

func TestRunReplaceCommentsLogsStateComparison(t *testing.T) {
	t.Parallel()

	commentsPath := filepath.Join(t.TempDir(), "comments.json")
	require.NoError(t, os.WriteFile(commentsPath, []byte(`[{"path":"a.go","line":7,"side":"RIGHT","body":"new comment"}]`), 0o644))

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:                 "ava-labs/avalanchego",
		PRNumber:             5168,
		UserLogin:            "maru",
		ReviewID:             "review-123",
		LastPublishedBody:    "live body",
		LastPublishedEntries: []DraftReviewEntry{{Kind: DraftReviewEntryKindNewThread, Path: "a.go", Line: 7, Side: reviewSideRight, Body: "old comment"}},
	}))

	var replaced bool
	server := newGraphQLTestServer(t, func(t *testing.T, query string, _ map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			if replaced {
				return graphQLPullRequestData("live body", []map[string]any{
					graphQLThreadComment("comment-2", "thread-2", "new comment", "a.go", 7),
				})
			}
			return graphQLPullRequestData("live body", []map[string]any{
				graphQLThreadComment("comment-1", "thread-1", "old comment", "a.go", 7),
			})
		case strings.Contains(query, "mutation DeletePendingReviewComment"):
			return map[string]any{
				"deletePullRequestReviewComment": map[string]any{"clientMutationId": "deleted"},
			}
		case strings.Contains(query, "mutation AddPendingReviewThread"):
			replaced = true
			return map[string]any{
				"addPullRequestReviewThread": map[string]any{
					"thread": map[string]any{
						"id": "thread-2",
						"comments": map[string]any{
							"nodes": []map[string]any{
								graphQLThreadComment("comment-2", "thread-2", "new comment", "a.go", 7),
							},
						},
					},
				},
			}
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL
	app.log = logging.NewLogger(
		"pendingreview",
		logging.NewWrappedCore(logging.Debug, nopWriteCloser{Writer: &stderr}, logging.JSON.ConsoleEncoder()),
	)

	require.NoError(t, app.Run(t.Context(), []string{
		"replace-comments",
		"--pr", "5168",
		"--comments-file", commentsPath,
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	logOutput := stderr.String()
	for _, expected := range []string{
		`"msg":"fetched live pending review for comment replace"`,
		`"msg":"loaded stored pending review state for comment replace"`,
		`"msg":"comparing stored and live managed entries"`,
		`"msg":"applying pending review comment replace"`,
		`"reviewID":"review-123"`,
		`"liveManagedEntryCount":1`,
		`"storedEntryCount":1`,
		`"desiredEntryCount":1`,
		`"entriesMatch":true`,
		`"existingCommentCount":1`,
	} {
		require.Contains(t, logOutput, expected)
	}
}
