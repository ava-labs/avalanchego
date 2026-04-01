package draftreview

import (
	"bytes"
	"context"
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
		require.Equal(t, "/repos/ava-labs/avalanchego/pulls/5168/reviews", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":123,"state":"PENDING","body":"test","html_url":"https://example.invalid/review/123","user":{"login":"maru"}}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL
	app.log = logging.NewLogger(
		"draftreview",
		logging.NewWrappedCore(logging.Debug, nopWriteCloser{Writer: &stderr}, logging.JSON.ConsoleEncoder()),
	)

	stateDir := t.TempDir()
	err := app.Run(t.Context(), []string{
		"create",
		"--repo", "ava-labs/avalanchego",
		"--pr", "5168",
		"--body", "test",
		"--config-dir", t.TempDir(),
		"--state-dir", stateDir,
	})
	require.NoError(t, err)

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
		`"reviewID":123`,
	} {
		require.Contains(t, logOutput, expected)
	}
}
