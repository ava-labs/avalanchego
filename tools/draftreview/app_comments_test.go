package draftreview

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

const (
	userPath              = "/user"
	reviewsPath           = "/repos/ava-labs/avalanchego/pulls/5168/reviews"
	reviewComments123Path = "/repos/ava-labs/avalanchego/pulls/5168/reviews/123/comments"
)

func TestRunGetIncludesComments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case userPath:
			_, _ = io.WriteString(w, `{"login":"maru"}`)
		case reviewsPath:
			_, _ = io.WriteString(w, `[{"id":123,"state":"PENDING","body":"body","html_url":"https://example.invalid/review/123","user":{"login":"maru"}}]`)
		case reviewComments123Path:
			_, _ = io.WriteString(w, `[{"id":1,"pull_request_review_id":123,"path":"a.go","line":7,"side":"RIGHT","body":"comment","user":{"login":"maru"}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	err := app.Run(t.Context(), []string{"get", "--pr", "5168", "--state-dir", t.TempDir(), "--config-dir", t.TempDir()})
	require.NoError(t, err)

	var review Review
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &review))
	require.Len(t, review.Comments, 1)
	require.Equal(t, "a.go", review.Comments[0].Path)
	require.Equal(t, "comment", review.Comments[0].Body)
}

func TestRunReplaceCommentsRecreatesPendingReviewWithLiveBody(t *testing.T) {
	t.Parallel()

	commentsPath := filepath.Join(t.TempDir(), "comments.json")
	require.NoError(t, os.WriteFile(commentsPath, []byte(`[{"path":"a.go","line":7,"side":"RIGHT","body":"new comment"}]`), 0o644))

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:                  "ava-labs/avalanchego",
		PRNumber:              5168,
		UserLogin:             "maru",
		ReviewID:              123,
		LastPublishedBody:     "live body",
		LastPublishedComments: []DraftReviewComment{{Path: "old.go", Line: 5, Side: "RIGHT", Body: "old comment"}},
	}))

	var createPayload struct {
		Body     string                   `json:"body"`
		Comments []map[string]interface{} `json:"comments"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == userPath:
			_, _ = io.WriteString(w, `{"login":"maru"}`)
		case r.Method == http.MethodGet && r.URL.Path == reviewsPath:
			_, _ = io.WriteString(w, `[{"id":123,"state":"PENDING","body":"live body","html_url":"https://example.invalid/review/123","user":{"login":"maru"}}]`)
		case r.Method == http.MethodGet && r.URL.Path == reviewComments123Path:
			_, _ = io.WriteString(w, `[{"id":1,"pull_request_review_id":123,"path":"old.go","line":5,"side":"RIGHT","body":"old comment","user":{"login":"maru"}}]`)
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/ava-labs/avalanchego/pulls/5168/reviews/123":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == reviewsPath:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createPayload))
			_, _ = io.WriteString(w, `{"id":456,"state":"PENDING","body":"live body","html_url":"https://example.invalid/review/456","user":{"login":"maru"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
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
	require.NoError(t, err)
	require.Equal(t, "live body", createPayload.Body)
	require.Len(t, createPayload.Comments, 1)
	require.Equal(t, "a.go", createPayload.Comments[0]["path"])

	loaded, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.NoError(t, err)
	require.Equal(t, int64(456), loaded.ReviewID)
	require.Len(t, loaded.LastPublishedComments, 1)
	require.Equal(t, "new comment", loaded.LastPublishedComments[0].Body)
}

func TestRunReplaceCommentsDetectsConflict(t *testing.T) {
	t.Parallel()

	commentsPath := filepath.Join(t.TempDir(), "comments.json")
	require.NoError(t, os.WriteFile(commentsPath, []byte(`[{"path":"a.go","line":7,"side":"RIGHT","body":"new comment"}]`), 0o644))

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:                  "ava-labs/avalanchego",
		PRNumber:              5168,
		UserLogin:             "maru",
		ReviewID:              123,
		LastPublishedBody:     "live body",
		LastPublishedComments: []DraftReviewComment{{Path: "old.go", Line: 5, Side: "RIGHT", Body: "old comment"}},
	}))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case userPath:
			_, _ = io.WriteString(w, `{"login":"maru"}`)
		case reviewsPath:
			_, _ = io.WriteString(w, `[{"id":123,"state":"PENDING","body":"live body","html_url":"https://example.invalid/review/123","user":{"login":"maru"}}]`)
		case reviewComments123Path:
			_, _ = io.WriteString(w, `[{"id":1,"pull_request_review_id":123,"path":"different.go","line":8,"side":"RIGHT","body":"user changed","user":{"login":"maru"}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
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
}
