// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
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

func TestRunGetIncludesComments(t *testing.T) {
	t.Parallel()

	server := newGraphQLTestServer(t, func(t *testing.T, query string, _ map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("body", []map[string]any{
				graphQLThreadComment("comment-1", "thread-1", "comment", "a.go", 7),
			})
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{"get", "--pr", "5168", "--state-dir", t.TempDir(), "--config-dir", t.TempDir()}))

	var review Review
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &review))
	require.Len(t, review.Comments, 1)
	require.Equal(t, DraftReviewEntryKindNewThread, review.Comments[0].Kind)
	require.Equal(t, "a.go", review.Comments[0].Path)
	require.Equal(t, "comment", review.Comments[0].Body)
}

func TestRunReplaceCommentsUpdatesPendingReviewEntries(t *testing.T) {
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
		LastPublishedEntries: []DraftReviewEntry{{Kind: DraftReviewEntryKindNewThread, Path: "old.go", Line: 5, Side: reviewSideRight, Body: "old comment"}},
	}))

	currentComments := []map[string]any{
		graphQLThreadComment("comment-1", "thread-1", "old comment", "old.go", 5),
	}
	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("live body", currentComments)
		case strings.Contains(query, "mutation DeletePendingReviewComment"):
			require.Equal(t, "comment-1", variables["commentID"])
			currentComments = nil
			return map[string]any{"deletePullRequestReviewComment": map[string]any{"clientMutationId": "deleted"}}
		case strings.Contains(query, "mutation AddPendingReviewThread"):
			input := variables["input"].(map[string]any)
			require.Equal(t, "review-123", input["pullRequestReviewId"])
			require.Equal(t, "a.go", input["path"])
			require.Equal(t, "new comment", input["body"])
			currentComments = []map[string]any{
				graphQLThreadComment("comment-2", "thread-2", "new comment", "a.go", 7),
			}
			return map[string]any{
				"addPullRequestReviewThread": map[string]any{
					"thread": map[string]any{
						"id": "thread-2",
						"comments": map[string]any{
							"nodes": currentComments,
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
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"replace-comments",
		"--pr", "5168",
		"--comments-file", commentsPath,
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	loaded, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.NoError(t, err)
	require.Equal(t, "review-123", loaded.ReviewID)
	require.Len(t, loaded.LastPublishedEntries, 1)
	require.Equal(t, "new comment", loaded.LastPublishedEntries[0].Body)
	require.Contains(t, stdout.String(), "Replaced comments for pending review 123")
}

func TestRunReplaceCommentsDetectsConflict(t *testing.T) {
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
		LastPublishedEntries: []DraftReviewEntry{{Kind: DraftReviewEntryKindNewThread, Path: "old.go", Line: 5, Side: reviewSideRight, Body: "old comment"}},
	}))

	server := newGraphQLTestServer(t, func(t *testing.T, query string, _ map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("live body", []map[string]any{
				graphQLThreadComment("comment-1", "thread-1", "user changed", "different.go", 8),
			})
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	err := app.Run(t.Context(), []string{
		"replace-comments",
		"--pr", "5168",
		"--comments-file", commentsPath,
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	})
	require.ErrorIs(t, err, ErrReviewCommentsConflict)
	require.Contains(t, err.Error(), "different.go:8")
	require.Contains(t, err.Error(), "old.go:5")
}

func TestRunReplaceCommentsCreatesReviewIfMissing(t *testing.T) {
	t.Parallel()

	commentsPath := filepath.Join(t.TempDir(), "comments.json")
	require.NoError(t, os.WriteFile(commentsPath, []byte(`[{"path":"a.go","line":7,"side":"RIGHT","body":"new comment"}]`), 0o644))

	var currentComments []map[string]any
	created := false
	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			if !created {
				return graphQLNoPendingReviewData()
			}
			return graphQLPullRequestData("Draft review for inline comments.", currentComments)
		case strings.Contains(query, "mutation CreatePendingReview"):
			require.Equal(t, "Draft review for inline comments.", variables["body"])
			created = true
			return map[string]any{
				"addPullRequestReview": map[string]any{
					"pullRequestReview": map[string]any{
						"id":         "review-123",
						"databaseId": 123,
						"state":      reviewStatePending,
						"body":       "Draft review for inline comments.",
						"url":        "https://example.invalid/review/123",
						"author":     map[string]any{"login": "maru"},
					},
				},
			}
		case strings.Contains(query, "mutation AddPendingReviewThread"):
			currentComments = []map[string]any{
				graphQLThreadComment("comment-1", "thread-1", "new comment", "a.go", 7),
			}
			return map[string]any{
				"addPullRequestReviewThread": map[string]any{
					"thread": map[string]any{
						"id": "thread-1",
						"comments": map[string]any{
							"nodes": currentComments,
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

	stateDir := t.TempDir()
	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"replace-comments",
		"--pr", "5168",
		"--comments-file", commentsPath,
		"--create-if-missing",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	store := NewStateStore(logging.NoLog{}, stateDir)
	loaded, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.NoError(t, err)
	require.Equal(t, "Draft review for inline comments.", loaded.LastPublishedBody)
	require.Len(t, loaded.LastPublishedEntries, 1)
	require.Contains(t, stdout.String(), "Replaced comments for pending review 123")
}

func TestRunUpsertCommentUpdatesSingleManagedComment(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:                 "ava-labs/avalanchego",
		PRNumber:             5168,
		UserLogin:            "maru",
		ReviewID:             "review-123",
		LastPublishedBody:    "live body",
		LastPublishedEntries: []DraftReviewEntry{{Kind: DraftReviewEntryKindNewThread, Path: "a.go", Line: 7, Side: reviewSideRight, Body: "old body"}},
	}))

	currentComments := []map[string]any{
		graphQLThreadComment("comment-1", "thread-1", "old body", "a.go", 7),
	}
	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("live body", currentComments)
		case strings.Contains(query, "mutation DeletePendingReviewComment"):
			require.Equal(t, "comment-1", variables["commentID"])
			currentComments = nil
			return map[string]any{"deletePullRequestReviewComment": map[string]any{"clientMutationId": "deleted"}}
		case strings.Contains(query, "mutation AddPendingReviewThread"):
			input := variables["input"].(map[string]any)
			require.Equal(t, "review-123", input["pullRequestReviewId"])
			require.Equal(t, "updated body", input["body"])
			currentComments = []map[string]any{
				graphQLThreadComment("comment-2", "thread-2", "updated body", "a.go", 7),
			}
			return map[string]any{
				"addPullRequestReviewThread": map[string]any{
					"thread": map[string]any{
						"id": "thread-2",
						"comments": map[string]any{
							"nodes": currentComments,
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
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"upsert-comment",
		"--pr", "5168",
		"--path", "a.go",
		"--line", "7",
		"--side", "RIGHT",
		"--body", "updated body",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	loaded, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.NoError(t, err)
	require.Len(t, loaded.LastPublishedEntries, 1)
	require.Equal(t, "updated body", loaded.LastPublishedEntries[0].Body)
	require.Contains(t, stdout.String(), "Upserted comment for pending review 123")
}

func TestRunUpsertCommentByCommentIDUsesStoredAnchor(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:                 "ava-labs/avalanchego",
		PRNumber:             5168,
		UserLogin:            "maru",
		ReviewID:             "review-123",
		LastPublishedBody:    "live body",
		LastPublishedEntries: []DraftReviewEntry{{Kind: DraftReviewEntryKindNewThread, Path: "a.go", Line: 7, Side: reviewSideRight, Body: "old body"}},
	}))

	currentComments := []map[string]any{
		graphQLThreadComment("comment-1", "thread-1", "old body", "a.go", 7),
	}
	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("live body", currentComments)
		case strings.Contains(query, "mutation DeletePendingReviewComment"):
			require.Equal(t, "comment-1", variables["commentID"])
			currentComments = nil
			return map[string]any{"deletePullRequestReviewComment": map[string]any{"clientMutationId": "deleted"}}
		case strings.Contains(query, "mutation AddPendingReviewThread"):
			input := variables["input"].(map[string]any)
			require.Equal(t, "a.go", input["path"])
			require.Equal(t, float64(7), input["line"])
			require.Equal(t, reviewSideRight, input["side"])
			require.Equal(t, "updated body", input["body"])
			currentComments = []map[string]any{
				graphQLThreadComment("comment-2", "thread-2", "updated body", "a.go", 7),
			}
			return map[string]any{
				"addPullRequestReviewThread": map[string]any{
					"thread": map[string]any{
						"id": "thread-2",
						"comments": map[string]any{
							"nodes": currentComments,
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

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"upsert-comment",
		"--pr", "5168",
		"--comment-id", "comment-1",
		"--body", "updated body",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))
}

func TestRunCreateReadsBodyFile(t *testing.T) {
	t.Parallel()

	bodyPath := filepath.Join(t.TempDir(), "body.txt")
	require.NoError(t, os.WriteFile(bodyPath, []byte("body from file\n"), 0o644))

	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("existing", nil)
		case strings.Contains(query, "mutation CreatePendingReview"):
			require.Equal(t, "body from file\n", variables["body"])
			return map[string]any{
				"addPullRequestReview": map[string]any{
					"pullRequestReview": map[string]any{
						"id":         "review-123",
						"databaseId": 123,
						"state":      reviewStatePending,
						"body":       "body from file\n",
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

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"create",
		"--pr", "5168",
		"--body-file", bodyPath,
		"--state-dir", t.TempDir(),
		"--config-dir", t.TempDir(),
	}))
}

func TestRunUpdateBodyReadsBodyFile(t *testing.T) {
	t.Parallel()

	bodyPath := filepath.Join(t.TempDir(), "body.txt")
	require.NoError(t, os.WriteFile(bodyPath, []byte("updated from file\n"), 0o644))

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru",
		ReviewID:          "review-123",
		LastPublishedBody: "live body",
	}))

	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLPullRequestData("live body", nil)
		case strings.Contains(query, "mutation UpdatePendingReviewBody"):
			require.Equal(t, "review-123", variables["reviewID"])
			require.Equal(t, "updated from file\n", variables["body"])
			return map[string]any{
				"updatePullRequestReview": map[string]any{
					"pullRequestReview": map[string]any{
						"id":         "review-123",
						"databaseId": 123,
						"state":      reviewStatePending,
						"body":       "updated from file\n",
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

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"update-body",
		"--pr", "5168",
		"--body-file", bodyPath,
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))
}

func TestRunGetStatePrintsStoredReviewState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:                 "ava-labs/avalanchego",
		PRNumber:             5168,
		UserLogin:            "maru",
		ReviewID:             "review-123",
		LastPublishedBody:    "stored body",
		LastPublishedEntries: []DraftReviewEntry{{Kind: DraftReviewEntryKindNewThread, Path: "a.go", Line: 7, Side: reviewSideRight, Body: "stored comment"}},
		HTMLURL:              "https://example.invalid/review/123",
	}))

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)

	require.NoError(t, app.Run(t.Context(), []string{
		"get-state",
		"--pr", "5168",
		"--user", "maru",
		"--state-dir", stateDir,
	}))

	var state ReviewState
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &state))
	require.Equal(t, "review-123", state.ReviewID)
	require.Equal(t, "stored body", state.LastPublishedBody)
	require.Len(t, state.LastPublishedEntries, 1)
}

func TestRunDeleteStateDeletesStoredReviewState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru",
		ReviewID:          "review-123",
		LastPublishedBody: "stored body",
	}))

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)

	require.NoError(t, app.Run(t.Context(), []string{
		"delete-state",
		"--pr", "5168",
		"--user", "maru",
		"--state-dir", stateDir,
	}))

	_, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.EqualError(t, err, "no stored review state for ava-labs/avalanchego#5168 as maru; run create first or use --force")
	require.Contains(t, stdout.String(), "Deleted stored review state")
}

func TestRunDeleteDeletesPendingReviewAndStoredReviewState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru",
		ReviewID:          "review-123",
		LastPublishedBody: "stored body",
	}))

	deleted := false
	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			if deleted {
				return graphQLNoPendingReviewData()
			}
			return graphQLPullRequestData("body", nil)
		case strings.Contains(query, "mutation DeletePendingReview"):
			require.Equal(t, "review-123", variables["reviewID"])
			deleted = true
			return map[string]any{
				"deletePullRequestReview": map[string]any{"clientMutationId": "deleted"},
			}
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"delete",
		"--pr", "5168",
		"--ensure-absent",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	_, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.EqualError(t, err, "no stored review state for ava-labs/avalanchego#5168 as maru; run create first or use --force")
	require.Contains(t, stdout.String(), "Deleted pending review 123")
}

func TestRunDeleteDeletesStoredStateWhenPendingReviewAlreadyAbsent(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru",
		ReviewID:          "review-123",
		LastPublishedBody: "stored body",
	}))

	server := newGraphQLTestServer(t, func(t *testing.T, query string, _ map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestContext"):
			return graphQLNoPendingReviewData()
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"delete",
		"--pr", "5168",
		"--ensure-absent",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	_, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.EqualError(t, err, "no stored review state for ava-labs/avalanchego#5168 as maru; run create first or use --force")
	require.Contains(t, stdout.String(), "No pending review found for ava-labs/avalanchego#5168 as maru; verified no pending review remains and cleared stored state if present.")
}

func newGraphQLTestServer(t *testing.T, handler func(t *testing.T, query string, variables map[string]any) any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/graphql", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)

		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": handler(t, payload.Query, payload.Variables),
		}))
	}))
}

func graphQLPullRequestData(body string, comments []map[string]any) map[string]any {
	reviewComments := make([]map[string]any, 0, len(comments))
	reviewThreads := make([]map[string]any, 0, len(comments))
	for _, comment := range comments {
		reviewComments = append(reviewComments, comment)
		reviewThreads = append(reviewThreads, map[string]any{
			"id": comment["threadID"],
			"comments": map[string]any{
				"nodes": []map[string]any{comment},
			},
		})
	}

	return map[string]any{
		"viewer": map[string]any{"login": "maru"},
		"repository": map[string]any{
			"pullRequest": map[string]any{
				"id": "pr-1",
				"reviewThreads": map[string]any{
					"nodes": reviewThreads,
				},
				"reviews": map[string]any{
					"nodes": []map[string]any{
						{
							"id":         "review-123",
							"databaseId": 123,
							"state":      reviewStatePending,
							"body":       body,
							"url":        "https://example.invalid/review/123",
							"author":     map[string]any{"login": "maru"},
							"comments": map[string]any{
								"nodes": reviewComments,
							},
						},
					},
				},
			},
		},
	}
}

func graphQLNoPendingReviewData() map[string]any {
	return map[string]any{
		"viewer": map[string]any{"login": "maru"},
		"repository": map[string]any{
			"pullRequest": map[string]any{
				"id": "pr-1",
				"reviewThreads": map[string]any{
					"nodes": []map[string]any{},
				},
				"reviews": map[string]any{
					"nodes": []map[string]any{},
				},
			},
		},
	}
}

func graphQLThreadComment(id string, threadID string, body string, path string, line int) map[string]any {
	return map[string]any{
		"id":            id,
		"threadID":      threadID,
		"body":          body,
		"path":          path,
		"line":          line,
		"diffSide":      reviewSideRight,
		"startLine":     0,
		"startDiffSide": "",
		"author":        map[string]any{"login": "maru"},
		"replyTo":       nil,
		"pullRequestReview": map[string]any{
			"id": "review-123",
		},
	}
}
