// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type failTokenProvider struct {
	t *testing.T
}

func (p failTokenProvider) Token(_ context.Context, _ string) (string, error) {
	p.t.Fatalf("proxy mode should not acquire a local GitHub token")
	return "", nil
}

func TestProxyModeRoutesCommandSurfaceThroughProxy(t *testing.T) {
	tests := map[string]struct {
		name            string
		args            []string
		expectedCommand string
		checkCommand    func(t *testing.T, cmd command)
	}{
		"version": {
			name:            "version",
			args:            []string{"version"},
			expectedCommand: "version",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				_, ok := cmd.(versionCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
			},
		},
		"create": {
			name:            "create",
			args:            []string{"create", "--pr", "5168", "--body", "draft body", "--json"},
			expectedCommand: "create",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(createCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
				require.Equal(t, "ava-labs/avalanchego", decoded.Repo)
				require.Equal(t, 5168, decoded.PRNumber)
				require.Equal(t, "draft body", decoded.Body)
				require.True(t, decoded.JSON)
				require.Empty(t, decoded.BodyFile)
			},
		},
		"get": {
			name:            "get",
			args:            []string{"get", "--pr", "5168", "--pretty"},
			expectedCommand: "get",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(getCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
				require.Equal(t, 5168, decoded.PRNumber)
				require.True(t, decoded.Pretty)
			},
		},
		"update-body": {
			name:            "update-body body-file resolved client-side",
			args:            []string{"update-body", "--pr", "5168", "--body-file", "__BODY_FILE__", "--force", "--json"},
			expectedCommand: "update-body",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(updateBodyCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
				require.Equal(t, "body from file\n", decoded.Body)
				require.Empty(t, decoded.BodyFile)
				require.True(t, decoded.Force)
				require.True(t, decoded.JSON)
			},
		},
		"replace-comments": {
			name:            "replace-comments comments-file resolved client-side",
			args:            []string{"replace-comments", "--pr", "5168", "--comments-file", "__COMMENTS_FILE__", "--review-body-file", "__REVIEW_BODY_FILE__", "--create-if-missing"},
			expectedCommand: "replace-comments",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(replaceCommentsCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
				require.Equal(t, "review body from file\n", decoded.ReviewBody)
				require.Empty(t, decoded.ReviewBodyFile)
				require.Empty(t, decoded.CommentsFile)
				require.Len(t, decoded.Comments, 1)
				comment := decoded.Comments[0]
				require.Equal(t, "a.go", comment.Path)
				require.Equal(t, 7, comment.Line)
				require.Equal(t, "RIGHT", comment.Side)
				require.Equal(t, "new comment", comment.Body)
				require.True(t, decoded.CreateIfMissing)
			},
		},
		"upsert-comment": {
			name:            "upsert-comment body-file resolved client-side",
			args:            []string{"upsert-comment", "--pr", "5168", "--path", "a.go", "--line", "7", "--side", "RIGHT", "--body-file", "__BODY_FILE__", "--create-if-missing"},
			expectedCommand: "upsert-comment",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(upsertCommentCommand)
				require.True(t, ok)
				require.Equal(t, "a.go", decoded.Path)
				require.Equal(t, 7, decoded.Line)
				require.Equal(t, "RIGHT", decoded.Side)
				require.Equal(t, "body from file\n", decoded.Body)
				require.Empty(t, decoded.BodyFile)
				require.True(t, decoded.CreateIfMissing)
			},
		},
		"delete": {
			name:            "delete",
			args:            []string{"delete", "--pr", "5168", "--ensure-absent", "--json"},
			expectedCommand: "delete",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(deleteCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
				require.True(t, decoded.EnsureAbsent)
				require.True(t, decoded.JSON)
			},
		},
		"get-state": {
			name:            "get-state",
			args:            []string{"get-state", "--pr", "5168", "--user", "maru", "--pretty"},
			expectedCommand: "get-state",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(getStateCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
				require.Equal(t, "maru", decoded.UserLogin)
				require.True(t, decoded.Pretty)
			},
		},
		"delete-state": {
			name:            "delete-state",
			args:            []string{"delete-state", "--pr", "5168", "--user", "maru", "--json"},
			expectedCommand: "delete-state",
			checkCommand: func(t *testing.T, cmd command) {
				t.Helper()
				decoded, ok := cmd.(deleteStateCommand)
				require.True(t, ok, "unexpected command type %T", cmd)
				require.Equal(t, "maru", decoded.UserLogin)
				require.True(t, decoded.JSON)
			},
		},
	}

	for _, spec := range proxyCommandRegistry() {
		tt, ok := tests[spec.name]
		require.Truef(t, ok, "missing proxy integration case for command %q", spec.name)

		t.Run(tt.name, func(t *testing.T) {
			bodyFile := filepath.Join(t.TempDir(), "body.txt")
			require.NoError(t, os.WriteFile(bodyFile, []byte("body from file\n"), 0o644))
			reviewBodyFile := filepath.Join(t.TempDir(), "review-body.txt")
			require.NoError(t, os.WriteFile(reviewBodyFile, []byte("review body from file\n"), 0o644))
			commentsFile := filepath.Join(t.TempDir(), "comments.json")
			require.NoError(t, os.WriteFile(commentsFile, []byte(`[{"path":"a.go","line":7,"side":"RIGHT","body":"new comment"}]`), 0o644))

			args := append([]string(nil), tt.args...)
			for i := range args {
				switch args[i] {
				case "__BODY_FILE__":
					args[i] = bodyFile
				case "__REVIEW_BODY_FILE__":
					args[i] = reviewBodyFile
				case "__COMMENTS_FILE__":
					args[i] = commentsFile
				}
			}

			var seen bool
			server := httptest.NewServer(proxyHandler{
				allowedRepo: defaultRepo,
				execute: func(_ context.Context, cmd command, stdout io.Writer, _ io.Writer) error {
					seen = true
					require.Equal(t, tt.expectedCommand, commandName(cmd))
					tt.checkCommand(t, cmd)
					_, err := fmt.Fprintf(stdout, "proxied %s\n", tt.expectedCommand)
					return err
				},
			})
			defer server.Close()

			t.Setenv("GH_PENDING_REVIEW_PROXY_URL", server.URL)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := NewApp(strings.NewReader(""), &stdout, &stderr)
			app.tokenProvider = failTokenProvider{t: t}

			err := app.Run(t.Context(), args)
			require.NoError(t, err)
			require.True(t, seen, "expected request to be sent to proxy")
			require.Equal(t, fmt.Sprintf("proxied %s\n", tt.expectedCommand), stdout.String())
			require.Empty(t, stderr.String())
		})
	}
}

func TestProxyModeRejectsVersionMismatch(t *testing.T) {
	server := httptest.NewServer(NewProxyHandler())
	defer server.Close()
	t.Setenv("GH_PENDING_REVIEW_PROXY_URL", server.URL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.tokenProvider = failTokenProvider{t: t}
	app.httpClient = rewriteProxyVersionDoer{
		t:      t,
		target: server.Client(),
	}

	err := app.Run(t.Context(), []string{"version"})
	require.EqualError(t, err, "proxy command failed with exit code 1")
	require.Empty(t, stdout.String())
	require.Equal(t, "incompatible pending-review proxy version: client=0 server=1\n", stderr.String())
}

func TestProxyModeRejectsDisallowedRepoBeforeExecution(t *testing.T) {
	server := httptest.NewServer(NewProxyHandler())
	defer server.Close()
	t.Setenv("GH_PENDING_REVIEW_PROXY_URL", server.URL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.tokenProvider = failTokenProvider{t: t}

	err := app.Run(t.Context(), []string{"get", "--repo", "octo/example", "--pr", "5168"})
	require.EqualError(t, err, "proxy command failed with exit code 1")
	require.Empty(t, stdout.String())
	require.Equal(t, "proxy only permits repo ava-labs/avalanchego\n", stderr.String())
}

type rewriteProxyVersionDoer struct {
	t      *testing.T
	target *http.Client
}

func (d rewriteProxyVersionDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()

	body, err := io.ReadAll(req.Body)
	require.NoError(d.t, err)
	require.NoError(d.t, req.Body.Close())

	var envelope proxyRequestEnvelope
	require.NoError(d.t, json.Unmarshal(body, &envelope))
	envelope.Version = 0

	encoded, err := json.Marshal(envelope)
	require.NoError(d.t, err)

	rewrittenReq, err := http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), bytes.NewReader(encoded))
	require.NoError(d.t, err)
	rewrittenReq.Header = req.Header.Clone()

	return d.target.Do(rewrittenReq)
}
