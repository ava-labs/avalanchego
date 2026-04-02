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
