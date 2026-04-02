// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tools/pendingreview"
	"github.com/ava-labs/avalanchego/tools/skilltest"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestPendingReviewGetState(t *testing.T) {
	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			workDir := newPendingReviewTestRepo(t)
			logPath := installPendingReviewWrapper(t, workDir)

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   workDir,
				Prompt: "Inspect my local pending review state for PR 123 as github " +
					"login octocat. Do not change anything.",
				Timeout: 2 * time.Minute,
			})

			require.Equal(t, 0, result.ExitCode)

			invocation, err := os.ReadFile(logPath)
			require.NoError(t, err)
			got := string(invocation)
			require.Contains(t, got, "get-state")
			require.Contains(t, got, "--pr")
			require.Contains(t, got, "123")
			require.Contains(t, got, "--user")
			require.Contains(t, got, "octocat")
			require.Contains(t, got, "--state-dir")
			require.NotContains(t, got, "delete")
			require.NotContains(t, got, "update-body")
			require.NotContains(t, got, "replace-comments")
		})
	}
}

func TestPendingReviewCreateBodyEndToEnd(t *testing.T) {
	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			repoRoot := repoRoot(t)
			xdgConfigHome := filepath.Join(t.TempDir(), "config-home")
			xdgStateHome := filepath.Join(t.TempDir(), "state-home")
			configDir := filepath.Join(xdgConfigHome, "gh-pending-review")
			stateDir := filepath.Join(xdgStateHome, "gh-pending-review")
			require.NoError(t, os.MkdirAll(configDir, 0o755))

			backend := newFakePendingReviewBackend("octocat")
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Create a pending review on PR 123 with body foo. " +
					`Use --config-dir "$XDG_CONFIG_HOME/gh-pending-review" and ` +
					`--state-dir "$XDG_STATE_HOME/gh-pending-review". ` +
					"Do not submit the review.",
				Timeout: 3 * time.Minute,
				Env: map[string]string{
					"XDG_CONFIG_HOME":            xdgConfigHome,
					"XDG_STATE_HOME":             xdgStateHome,
					"GH_PENDING_REVIEW_BASE_URL": server.URL,
				},
				BinWrappers: map[string]string{
					"gh": "#!/bin/sh\nprintf 'test-token\\n'\n",
				},
			})

			require.Equal(t, 0, result.ExitCode)

			review := backend.review()
			require.Equal(t, "foo", review.Body)
			require.Equal(t, "review-123", review.ID)
			require.Equal(t, "octocat", review.Author.Login)
			require.Equal(t, []string{"PullRequestContext", "CreatePendingReview"}, backend.operations())

			store := pendingreview.NewStateStore(logging.NoLog{}, stateDir)
			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, "foo", stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
		})
	}
}

func TestPendingReviewReadBodyEndToEnd(t *testing.T) {
	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			repoRoot := repoRoot(t)
			xdgConfigHome := filepath.Join(t.TempDir(), "config-home")
			xdgStateHome := filepath.Join(t.TempDir(), "state-home")
			configDir := filepath.Join(xdgConfigHome, "gh-pending-review")
			stateDir := filepath.Join(xdgStateHome, "gh-pending-review")
			require.NoError(t, os.MkdirAll(configDir, 0o755))

			backend := newFakePendingReviewBackend("octocat")
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft v1",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Read the current pending review body on PR 123 and tell me exactly what it is. " +
					`Use --config-dir "$XDG_CONFIG_HOME/gh-pending-review" and ` +
					`--state-dir "$XDG_STATE_HOME/gh-pending-review". ` +
					"Do not change anything.",
				Timeout: 3 * time.Minute,
				Env: map[string]string{
					"XDG_CONFIG_HOME":            xdgConfigHome,
					"XDG_STATE_HOME":             xdgStateHome,
					"GH_PENDING_REVIEW_BASE_URL": server.URL,
				},
				BinWrappers: map[string]string{
					"gh": "#!/bin/sh\nprintf 'test-token\\n'\n",
				},
			})

			require.Equal(t, 0, result.ExitCode)

			review := backend.review()
			require.Equal(t, "draft v1", review.Body)
			require.Equal(t, []string{"Viewer", "PullRequestContext"}, backend.operations())

			_, err := os.Stat(stateFilePath(stateDir, "ava-labs/avalanchego", "octocat", 123))
			require.ErrorIs(t, err, os.ErrNotExist)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, output, "draft v1")
		})
	}
}

func TestPendingReviewUpdateBodyEndToEnd(t *testing.T) {
	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			repoRoot := repoRoot(t)
			xdgConfigHome := filepath.Join(t.TempDir(), "config-home")
			xdgStateHome := filepath.Join(t.TempDir(), "state-home")
			configDir := filepath.Join(xdgConfigHome, "gh-pending-review")
			stateDir := filepath.Join(xdgStateHome, "gh-pending-review")
			require.NoError(t, os.MkdirAll(configDir, 0o755))

			store := pendingreview.NewStateStore(logging.NoLog{}, stateDir)
			require.NoError(t, store.Save(pendingreview.ReviewState{
				Repo:              "ava-labs/avalanchego",
				PRNumber:          123,
				UserLogin:         "octocat",
				ReviewID:          "review-123",
				LastPublishedBody: "draft v1",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
					Body: "existing comment",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend("octocat")
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft v1",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Replace the current pending review body on PR 123 with bar. " +
					`Use --config-dir "$XDG_CONFIG_HOME/gh-pending-review" and ` +
					`--state-dir "$XDG_STATE_HOME/gh-pending-review". ` +
					"Do not submit the review.",
				Timeout: 3 * time.Minute,
				Env: map[string]string{
					"XDG_CONFIG_HOME":            xdgConfigHome,
					"XDG_STATE_HOME":             xdgStateHome,
					"GH_PENDING_REVIEW_BASE_URL": server.URL,
				},
				BinWrappers: map[string]string{
					"gh": "#!/bin/sh\nprintf 'test-token\\n'\n",
				},
			})

			require.Equal(t, 0, result.ExitCode)

			review := backend.review()
			require.Equal(t, "bar", review.Body)
			require.Equal(t, []string{"Viewer", "PullRequestContext", "UpdatePendingReviewBody"}, backend.operations())

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, "bar", stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
			require.Len(t, stored.LastPublishedEntries, 1)
			require.Equal(t, "existing comment", stored.LastPublishedEntries[0].Body)
		})
	}
}

func newPendingReviewTestRepo(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "bin"), 0o755))
	require.NoError(t, exec.Command("git", "init", "-q", workDir).Run())
	return workDir
}

func installPendingReviewWrapper(t *testing.T, workDir string) string {
	t.Helper()

	logPath := filepath.Join(workDir, "gh-pending-review.log")
	script := strings.Join([]string{
		"#!/bin/sh",
		"printf -- '---\\n' >>" + shellQuote(logPath),
		"printf '%s\\n' \"$@\" >>" + shellQuote(logPath),
		"cat <<'EOF'",
		"{\"body\":\"Saved draft review body\",\"comments\":[]}",
		"EOF",
	}, "\n") + "\n"

	wrapperPath := filepath.Join(workDir, "bin", "gh-pending-review")
	require.NoError(t, os.WriteFile(wrapperPath, []byte(script), 0o755))
	return logPath
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", ".."))
}

func readOutput(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func stateFilePath(stateDir string, repo string, userLogin string, prNumber int) string {
	return filepath.Join(
		stateDir,
		strings.ReplaceAll(repo, "/", string(filepath.Separator)),
		userLogin,
		fmt.Sprintf("%d.json", prNumber),
	)
}

type fakePendingReviewBackend struct {
	mu           sync.Mutex
	viewer       string
	liveReview   *fakeReview
	requestedOps []string
}

type fakeReview struct {
	ID         string
	DatabaseID int
	Body       string
	State      string
	URL        string
	Author     fakeUser
}

type fakeUser struct {
	Login string
}

func newFakePendingReviewBackend(viewer string) *fakePendingReviewBackend {
	return &fakePendingReviewBackend{viewer: viewer}
}

func (b *fakePendingReviewBackend) seedReview(review fakeReview) {
	b.mu.Lock()
	defer b.mu.Unlock()

	reviewCopy := review
	b.liveReview = &reviewCopy
}

func (b *fakePendingReviewBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/graphql" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := b.handle(payload.Query, payload.Variables)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": response}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (b *fakePendingReviewBackend) handle(query string, variables map[string]any) (map[string]any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch {
	case strings.Contains(query, "query Viewer"):
		b.requestedOps = append(b.requestedOps, "Viewer")
		return map[string]any{
			"viewer": map[string]any{"login": b.viewer},
		}, nil
	case strings.Contains(query, "query PullRequestContext"):
		b.requestedOps = append(b.requestedOps, "PullRequestContext")
		return map[string]any{
			"viewer": map[string]any{"login": b.viewer},
			"repository": map[string]any{
				"pullRequest": map[string]any{
					"id": "pr-123",
					"reviewThreads": map[string]any{
						"nodes": []map[string]any{},
					},
					"reviews": map[string]any{
						"nodes": b.reviewNodes(),
					},
				},
			},
		}, nil
	case strings.Contains(query, "mutation CreatePendingReview"):
		b.requestedOps = append(b.requestedOps, "CreatePendingReview")
		body, _ := variables["body"].(string)
		b.liveReview = &fakeReview{
			ID:         "review-123",
			DatabaseID: 123,
			Body:       body,
			State:      "PENDING",
			URL:        "https://example.invalid/review/123",
			Author: fakeUser{
				Login: b.viewer,
			},
		}
		return map[string]any{
			"addPullRequestReview": map[string]any{
				"pullRequestReview": map[string]any{
					"id":         b.liveReview.ID,
					"databaseId": b.liveReview.DatabaseID,
					"state":      b.liveReview.State,
					"body":       b.liveReview.Body,
					"url":        b.liveReview.URL,
					"author": map[string]any{
						"login": b.liveReview.Author.Login,
					},
				},
			},
		}, nil
	case strings.Contains(query, "mutation UpdatePendingReviewBody"):
		b.requestedOps = append(b.requestedOps, "UpdatePendingReviewBody")
		if b.liveReview == nil {
			return nil, errors.New("unexpected update with no live review")
		}
		reviewID, _ := variables["reviewID"].(string)
		if reviewID != b.liveReview.ID {
			return nil, fmt.Errorf("unexpected review id %q", reviewID)
		}
		body, _ := variables["body"].(string)
		b.liveReview.Body = body
		return map[string]any{
			"updatePullRequestReview": map[string]any{
				"pullRequestReview": map[string]any{
					"id":         b.liveReview.ID,
					"databaseId": b.liveReview.DatabaseID,
					"state":      b.liveReview.State,
					"body":       b.liveReview.Body,
					"url":        b.liveReview.URL,
					"author": map[string]any{
						"login": b.liveReview.Author.Login,
					},
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (b *fakePendingReviewBackend) reviewNodes() []map[string]any {
	if b.liveReview == nil {
		return []map[string]any{}
	}
	return []map[string]any{
		{
			"id":         b.liveReview.ID,
			"databaseId": b.liveReview.DatabaseID,
			"state":      b.liveReview.State,
			"body":       b.liveReview.Body,
			"url":        b.liveReview.URL,
			"author": map[string]any{
				"login": b.liveReview.Author.Login,
			},
			"comments": map[string]any{
				"nodes": []map[string]any{},
			},
		},
	}
}

func (b *fakePendingReviewBackend) review() fakeReview {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.liveReview == nil {
		panic("fake pending review was never created")
	}
	return *b.liveReview
}

func (b *fakePendingReviewBackend) operations() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.requestedOps)
}
