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

func TestPendingReviewDeleteStateLocalOnly(t *testing.T) {
	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			repoRoot := repoRoot(t)
			xdgConfigHome := filepath.Join(t.TempDir(), "config-home")
			stateDir := filepath.Join(t.TempDir(), "state-home", "gh-pending-review")
			require.NoError(t, os.MkdirAll(filepath.Join(xdgConfigHome, "gh-pending-review"), 0o755))

			store := pendingreview.NewStateStore(logging.NoLog{}, stateDir)
			require.NoError(t, store.Save(pendingreview.ReviewState{
				Repo:              "ava-labs/avalanchego",
				PRNumber:          123,
				UserLogin:         "octocat",
				ReviewID:          "review-123",
				LastPublishedBody: "stale draft",
				HTMLURL:           "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "live draft should remain untouched",
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
				Prompt: "Delete my local pending review state for PR 123 as github " +
					"login octocat because the local state is stale. Do not touch GitHub. " +
					`Use --config-dir "$XDG_CONFIG_HOME/gh-pending-review" and ` +
					`--state-dir "$XDG_STATE_HOME/gh-pending-review".`,
				Timeout: 2 * time.Minute,
				Env: map[string]string{
					"XDG_CONFIG_HOME":            xdgConfigHome,
					"XDG_STATE_HOME":             filepath.Dir(stateDir),
					"GH_PENDING_REVIEW_BASE_URL": server.URL,
				},
				BinWrappers: map[string]string{
					"gh": "#!/bin/sh\nprintf 'test-token\\n'\n",
				},
			})

			require.Equal(t, 0, result.ExitCode)
			require.Empty(t, backend.operations())
			require.True(t, backend.hasReview())

			_, err := os.Stat(stateFilePath(stateDir, "ava-labs/avalanchego", "octocat", 123))
			require.ErrorIs(t, err, os.ErrNotExist)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, strings.ToLower(output), "deleted")
			require.Contains(t, strings.ToLower(output), "local state")
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

			backend := newFakePendingReviewBackend()
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

func TestPendingReviewInspectBeforeMutatingStaysReadOnly(t *testing.T) {
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
					Body: "existing finding",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft v2 from GitHub",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body:     "live finding from GitHub",
					Path:     "snow/engine.go",
					Line:     7,
					Side:     "RIGHT",
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Before changing anything on PR 123, inspect the current pending review body " +
					"and inline comments and tell me what is there. Do not mutate the draft until I confirm. " +
					`Use --config-dir "$XDG_CONFIG_HOME/gh-pending-review" and ` +
					`--state-dir "$XDG_STATE_HOME/gh-pending-review".`,
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
			require.Equal(t, "draft v2 from GitHub", review.Body)
			require.Len(t, review.Comments, 1)
			require.Equal(t, "live finding from GitHub", review.Comments[0].Body)
			require.Equal(t, []string{"Viewer", "PullRequestContext"}, backend.operations())

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "draft v1", stored.LastPublishedBody)
			require.Len(t, stored.LastPublishedEntries, 1)
			require.Equal(t, "existing finding", stored.LastPublishedEntries[0].Body)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, output, "draft v2 from GitHub")
			require.Contains(t, output, "live finding from GitHub")
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

			backend := newFakePendingReviewBackend()
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

func TestPendingReviewDeleteEndToEnd(t *testing.T) {
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
				LastPublishedBody: "draft body",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
					Body: "existing finding",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft body",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body:     "existing finding",
					Path:     "snow/engine.go",
					Line:     7,
					Side:     "RIGHT",
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Delete my pending review on PR 123 and clean up any local pending review state. " +
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

			require.False(t, backend.hasReview())
			require.Equal(t, []string{"Viewer", "PullRequestContext", "DeletePendingReview"}, backend.operations())

			_, err := os.Stat(stateFilePath(stateDir, "ava-labs/avalanchego", "octocat", 123))
			require.ErrorIs(t, err, os.ErrNotExist)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, strings.ToLower(output), "deleted")
			require.Contains(t, strings.ToLower(output), "pending review")
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

			backend := newFakePendingReviewBackend()
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

func TestPendingReviewReconcileBodyAfterExternalEdit(t *testing.T) {
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
				LastPublishedBody: "Original intro.\n\nShared context from the previous draft.",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
					Body: "existing comment",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body: "Original intro.\n\nShared context from the previous draft.\n\n" +
					"Human note: keep this context intact.\n" +
					"!! Replace the first sentence with \"Updated intro after human edit.\" " +
					"Preserve the human note and the shared context, then continue from there.",
				State: "PENDING",
				URL:   "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body:     "existing comment",
					Path:     "snow/engine.go",
					Line:     7,
					Side:     "RIGHT",
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "The pending review body on PR 123 was edited in GitHub by a human. " +
					"Continue the draft by applying any !! instruction in the live review body, " +
					"preserving the human note and existing shared context. " +
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
			require.Equal(t,
				"Updated intro after human edit.\n\nShared context from the previous draft.\n\nHuman note: keep this context intact.",
				strings.TrimSpace(review.Body),
			)
			ops := backend.operations()
			require.Equal(t, 1, countStrings(ops, "UpdatePendingReviewBody"))
			require.GreaterOrEqual(t, countStrings(ops, "PullRequestContext"), 2)
			updateIndex := slices.Index(ops, "UpdatePendingReviewBody")
			require.NotEqual(t, -1, updateIndex)
			require.Contains(t, ops[:updateIndex], "PullRequestContext")

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, review.Body, stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
			require.Len(t, stored.LastPublishedEntries, 1)
			require.Equal(t, "existing comment", stored.LastPublishedEntries[0].Body)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, output, "Updated intro after human edit.")
			require.Contains(t, strings.ToLower(output), "human note")
		})
	}
}

func TestPendingReviewReconcileCommentsAfterExternalEdit(t *testing.T) {
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
				LastPublishedBody: "draft body",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
					Body: "Original finding.\n\nShared context from the previous draft.",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft body",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body: "Original finding.\n\nShared context from the previous draft.\n\n" +
						"Human note: keep this context intact.\n" +
						"!! Replace the first sentence with \"Updated finding after human edit.\" " +
						"Preserve the human note and the shared context, then continue from there.",
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "The pending review inline comments on PR 123 were edited in GitHub by a human. " +
					"Stay at the ./bin/gh-pending-review CLI boundary and do not inspect repo source or tests. " +
					"Run get first, inspect the live pending review comments, and reconcile the existing managed comment instead of drafting from scratch. " +
					"Apply any !! instruction in the live managed comment by updating only the first sentence, removing the !! instruction text from the final body, and preserving the human note, shared context, and existing anchor fields. " +
					"Then replace the managed comment set with that reconciled single comment using the default new-thread comments-file shape with only path, line, side, and body fields, and keep the existing top-level review body unchanged. " +
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
			require.Equal(t, "draft body", review.Body)
			require.Len(t, review.Comments, 1)
			require.Equal(t,
				"Updated finding after human edit.\n\nShared context from the previous draft.\n\nHuman note: keep this context intact.",
				strings.TrimSpace(review.Comments[0].Body),
			)
			require.Equal(t, "snow/engine.go", review.Comments[0].Path)
			require.Equal(t, 7, review.Comments[0].Line)
			require.Equal(t, "RIGHT", review.Comments[0].Side)

			ops := backend.operations()
			require.Equal(t, 1, countStrings(ops, "DeletePendingReviewComment"))
			require.Equal(t, 1, countStrings(ops, "AddPendingReviewThread"))
			require.GreaterOrEqual(t, countStrings(ops, "PullRequestContext"), 2)
			deleteIndex := slices.Index(ops, "DeletePendingReviewComment")
			require.NotEqual(t, -1, deleteIndex)
			require.Contains(t, ops[:deleteIndex], "PullRequestContext")

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, "draft body", stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
			require.Len(t, stored.LastPublishedEntries, 1)
			require.Equal(t, pendingreview.DraftReviewEntryKindNewThread, stored.LastPublishedEntries[0].Kind)
			require.Equal(t, "snow/engine.go", stored.LastPublishedEntries[0].Path)
			require.Equal(t, 7, stored.LastPublishedEntries[0].Line)
			require.Equal(t,
				"Updated finding after human edit.\n\nShared context from the previous draft.\n\nHuman note: keep this context intact.",
				stored.LastPublishedEntries[0].Body,
			)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, output, "Updated finding after human edit.")
			require.Contains(t, strings.ToLower(output), "human note")
		})
	}
}

func TestPendingReviewConflictBodyRequiresReadBeforeForcedOverwrite(t *testing.T) {
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
				LastPublishedBody: "Original intro.\n\nShared context from the previous draft.",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
					Body: "existing comment",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body: "Original intro.\n\nShared context from the previous draft.\n\n" +
					"Human note: keep this context intact.",
				State: "PENDING",
				URL:   "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body:     "existing comment",
					Path:     "snow/engine.go",
					Line:     7,
					Side:     "RIGHT",
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Update the pending review body on PR 123 so it starts with " +
					`"Updated intro after conflict." while preserving any live human note ` +
					"and the existing shared context. If the draft changed in GitHub, reconcile " +
					"those live edits rather than clobbering them. " +
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
			require.Equal(t,
				"Updated intro after conflict.\n\nShared context from the previous draft.\n\nHuman note: keep this context intact.",
				strings.TrimSpace(review.Body),
			)

			ops := backend.operations()
			require.Equal(t, 1, countStrings(ops, "UpdatePendingReviewBody"))
			require.GreaterOrEqual(t, countStrings(ops, "PullRequestContext"), 2, "ops=%v", ops)
			updateIndex := slices.Index(ops, "UpdatePendingReviewBody")
			require.NotEqual(t, -1, updateIndex)
			require.GreaterOrEqual(t, countStrings(ops[:updateIndex], "PullRequestContext"), 2, "ops=%v", ops)

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, review.Body, stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
			require.Len(t, stored.LastPublishedEntries, 1)
			require.Equal(t, "existing comment", stored.LastPublishedEntries[0].Body)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, strings.ToLower(output), "conflict")
			require.Contains(t, output, "--force")
			require.Contains(t, output, "Updated intro after conflict.")
			require.Contains(t, strings.ToLower(output), "human note")
		})
	}
}

func TestPendingReviewConflictCommentsRequiresReadBeforeForcedOverwrite(t *testing.T) {
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
				LastPublishedBody: "draft body",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
					Body: "Original finding.\n\nShared context from the previous draft.",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft body",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body: "Original finding.\n\nShared context from the previous draft.\n\n" +
						"Human note: keep this context intact.",
					Path: "snow/engine.go",
					Line: 7,
					Side: "RIGHT",
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Replace the managed pending review comments on PR 123 with a single " +
					"comment on snow/engine.go line 7 on the RIGHT side. The comment should start " +
					`with "Updated finding after conflict." while preserving any live human note ` +
					"and the existing shared context. Stay at the ./bin/gh-pending-review CLI boundary and do not inspect repo source or tests. Write the replacement comments file using the default new-thread shape with only path, line, side, and body fields. If the draft changed in GitHub, run get, use the live pending review comment as the source of truth, reconcile those live edits rather than clobbering them, and only then retry replace-comments with --force. Keep the top-level review body unchanged. " +
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
			require.Equal(t, "draft body", review.Body)
			require.Len(t, review.Comments, 1)
			require.Equal(t,
				"Updated finding after conflict.\n\nShared context from the previous draft.\n\nHuman note: keep this context intact.",
				strings.TrimSpace(review.Comments[0].Body),
			)
			require.Equal(t, "snow/engine.go", review.Comments[0].Path)
			require.Equal(t, 7, review.Comments[0].Line)
			require.Equal(t, "RIGHT", review.Comments[0].Side)

			ops := backend.operations()
			require.Equal(t, 1, countStrings(ops, "DeletePendingReviewComment"))
			require.Equal(t, 1, countStrings(ops, "AddPendingReviewThread"))
			require.GreaterOrEqual(t, countStrings(ops, "PullRequestContext"), 2, "ops=%v", ops)
			deleteIndex := slices.Index(ops, "DeletePendingReviewComment")
			require.NotEqual(t, -1, deleteIndex)
			require.GreaterOrEqual(t, countStrings(ops[:deleteIndex], "PullRequestContext"), 2, "ops=%v", ops)

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, "draft body", stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
			require.Len(t, stored.LastPublishedEntries, 1)
			require.Equal(t, pendingreview.DraftReviewEntryKindNewThread, stored.LastPublishedEntries[0].Kind)
			require.Equal(t, "snow/engine.go", stored.LastPublishedEntries[0].Path)
			require.Equal(t, 7, stored.LastPublishedEntries[0].Line)
			require.Equal(t,
				"Updated finding after conflict.\n\nShared context from the previous draft.\n\nHuman note: keep this context intact.",
				stored.LastPublishedEntries[0].Body,
			)

			output := readOutput(t, result.OutputPath)
			require.Contains(t, strings.ToLower(output), "conflict")
			require.Contains(t, output, "--force")
			require.Contains(t, output, "Updated finding after conflict.")
			require.Contains(t, strings.ToLower(output), "human note")
		})
	}
}

func TestPendingReviewReplaceCommentsEndToEnd(t *testing.T) {
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
				LastPublishedBody: "draft body",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "old/file.go",
					Line: 5,
					Side: "RIGHT",
					Body: "old comment",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft body",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body:     "old comment",
					Path:     "old/file.go",
					Line:     5,
					Side:     "RIGHT",
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Replace the current managed pending review comments on PR 123 " +
					"with a single comment on snow/engine.go line 7 on the RIGHT side " +
					`saying "new comment". Keep the existing top-level review body unchanged. ` +
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
			require.Equal(t, "draft body", review.Body)
			require.Len(t, review.Comments, 1)
			require.Equal(t, "new comment", review.Comments[0].Body)
			require.Equal(t, "snow/engine.go", review.Comments[0].Path)
			require.Equal(t, 7, review.Comments[0].Line)
			require.Equal(t, "RIGHT", review.Comments[0].Side)
			require.Equal(t, []string{
				"Viewer",
				"PullRequestContext",
				"DeletePendingReviewComment",
				"AddPendingReviewThread",
				"PullRequestContext",
			}, backend.operations())

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, "draft body", stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
			require.Len(t, stored.LastPublishedEntries, 1)
			require.Equal(t, "new comment", stored.LastPublishedEntries[0].Body)
			require.Equal(t, "snow/engine.go", stored.LastPublishedEntries[0].Path)
			require.Equal(t, 7, stored.LastPublishedEntries[0].Line)
		})
	}
}

func TestPendingReviewReplaceCommentsWithThreadReplyEndToEnd(t *testing.T) {
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
				LastPublishedBody: "draft body",
				LastPublishedEntries: []pendingreview.DraftReviewEntry{{
					Kind: pendingreview.DraftReviewEntryKindNewThread,
					Path: "old/file.go",
					Line: 5,
					Side: "RIGHT",
					Body: "old comment",
				}},
				HTMLURL: "https://example.invalid/review/123",
			}))

			backend := newFakePendingReviewBackend()
			backend.seedReview(fakeReview{
				ID:         "review-123",
				DatabaseID: 123,
				Body:       "draft body",
				State:      "PENDING",
				URL:        "https://example.invalid/review/123",
				Author: fakeUser{
					Login: "octocat",
				},
				Comments: []fakeReviewComment{{
					ID:       "comment-1",
					ThreadID: "thread-1",
					Body:     "old comment",
					Path:     "old/file.go",
					Line:     5,
					Side:     "RIGHT",
				}},
				Threads: []fakeReviewThread{{
					ID: "thread-42",
					Comments: []fakeReviewComment{{
						ID:          "comment-2",
						ThreadID:    "thread-42",
						Body:        "existing thread root",
						Path:        "snow/engine.go",
						Line:        7,
						Side:        "RIGHT",
						AuthorLogin: "reviewer",
					}},
				}},
			})
			server := httptest.NewServer(backend)
			defer server.Close()

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   repoRoot,
				Prompt: "Write a comments file containing exactly " +
					`[{"kind":"thread_reply","thread_id":"thread-42","body":"new reply"}] ` +
					"and use the repo-local ./bin/gh-pending-review replace-comments command " +
					"to replace the current managed pending review comments on PR 123. " +
					"Keep the existing top-level review body unchanged. " +
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

			output := readOutput(t, result.OutputPath)
			review := backend.review()
			require.Equal(t, "draft body", review.Body)
			require.Len(t, review.Comments, 1, "ops=%v comments=%+v output=%s", backend.operations(), review.Comments, output)
			require.Contains(t, backend.operations(), "AddPendingReviewThreadReply")
			require.NotContains(t, backend.operations(), "AddPendingReviewThread")

			existingThread := review.commentsForThread("thread-42")
			require.Len(t, existingThread, 2)
			require.Equal(t, "existing thread root", existingThread[0].Body)
			require.Equal(t, "reviewer", existingThread[0].AuthorLogin)
			require.Equal(t, "new reply", existingThread[1].Body)
			require.Equal(t, "thread-42", existingThread[1].ThreadID)
			require.Equal(t, "comment-2", existingThread[1].ReplyToCommentID)
			require.Equal(t, "octocat", existingThread[1].AuthorLogin)

			stored, err := store.Load("ava-labs/avalanchego", "octocat", 123)
			require.NoError(t, err)
			require.Equal(t, "review-123", stored.ReviewID)
			require.Equal(t, "draft body", stored.LastPublishedBody)
			require.Equal(t, "https://example.invalid/review/123", stored.HTMLURL)
			require.Equal(t, []pendingreview.DraftReviewEntry{{
				Kind:             pendingreview.DraftReviewEntryKindThreadReply,
				ThreadID:         "thread-42",
				ReplyToCommentID: "comment-2",
				Body:             "new reply",
			}}, stored.LastPublishedEntries)
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

func countStrings(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
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
	nextID       int
}

type fakeReview struct {
	ID         string
	DatabaseID int
	Body       string
	State      string
	URL        string
	Author     fakeUser
	Comments   []fakeReviewComment
	Threads    []fakeReviewThread
}

type fakeReviewThread struct {
	ID       string
	Comments []fakeReviewComment
}

type fakeUser struct {
	Login string
}

type fakeReviewComment struct {
	ID               string
	ThreadID         string
	ReplyToCommentID string
	Body             string
	Path             string
	Line             int
	Side             string
	StartLine        int
	StartSide        string
	AuthorLogin      string
}

func newFakePendingReviewBackend() *fakePendingReviewBackend {
	return &fakePendingReviewBackend{
		viewer: "octocat",
		nextID: 1,
	}
}

func (b *fakePendingReviewBackend) seedReview(review fakeReview) {
	b.mu.Lock()
	defer b.mu.Unlock()

	reviewCopy := review
	reviewCopy.Comments = slices.Clone(review.Comments)
	reviewCopy.Threads = cloneFakeReviewThreads(review.Threads)
	b.liveReview = &reviewCopy
	for _, comment := range review.Comments {
		b.advanceNextID(comment.ID)
		b.advanceNextID(comment.ThreadID)
	}
	for _, thread := range review.Threads {
		b.advanceNextID(thread.ID)
		for _, comment := range thread.Comments {
			b.advanceNextID(comment.ID)
			b.advanceNextID(comment.ThreadID)
		}
	}
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
						"nodes": b.graphQLThreads(),
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
		b.advanceNextID(b.liveReview.ID)
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
	case strings.Contains(query, "mutation DeletePendingReviewComment"):
		b.requestedOps = append(b.requestedOps, "DeletePendingReviewComment")
		if b.liveReview == nil {
			return nil, errors.New("unexpected comment delete with no live review")
		}
		commentID, _ := variables["commentID"].(string)
		if !b.deleteComment(commentID) {
			return nil, fmt.Errorf("unexpected comment id %q", commentID)
		}
		return map[string]any{
			"deletePullRequestReviewComment": map[string]any{
				"clientMutationId": "deleted",
			},
		}, nil
	case strings.Contains(query, "mutation DeletePendingReview"):
		b.requestedOps = append(b.requestedOps, "DeletePendingReview")
		if b.liveReview == nil {
			return nil, errors.New("unexpected delete with no live review")
		}
		reviewID, _ := variables["reviewID"].(string)
		if reviewID != b.liveReview.ID {
			return nil, fmt.Errorf("unexpected review id %q", reviewID)
		}
		b.liveReview = nil
		return map[string]any{
			"deletePullRequestReview": map[string]any{
				"clientMutationId": "deleted",
			},
		}, nil
	case strings.Contains(query, "mutation AddPendingReviewThreadReply"):
		b.requestedOps = append(b.requestedOps, "AddPendingReviewThreadReply")
		if b.liveReview == nil {
			return nil, errors.New("unexpected thread reply with no live review")
		}
		reviewID, _ := variables["reviewID"].(string)
		if reviewID != b.liveReview.ID {
			return nil, fmt.Errorf("unexpected review id %q", reviewID)
		}
		threadID, _ := variables["threadID"].(string)
		comment, err := b.addThreadReplyComment(threadID, stringValue(variables["body"]))
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"addPullRequestReviewThreadReply": map[string]any{
				"comment": b.graphQLComment(comment),
			},
		}, nil
	case strings.Contains(query, "mutation AddPendingReviewThread"):
		b.requestedOps = append(b.requestedOps, "AddPendingReviewThread")
		if b.liveReview == nil {
			return nil, errors.New("unexpected thread create with no live review")
		}
		input, _ := variables["input"].(map[string]any)
		reviewID, _ := input["pullRequestReviewId"].(string)
		if reviewID != b.liveReview.ID {
			return nil, fmt.Errorf("unexpected review id %q", reviewID)
		}
		comment := b.addThreadComment(input)
		return map[string]any{
			"addPullRequestReviewThread": map[string]any{
				"thread": map[string]any{
					"id": comment.ThreadID,
					"comments": map[string]any{
						"nodes": []map[string]any{
							b.graphQLComment(comment),
						},
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
				"nodes": b.graphQLComments(),
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
	reviewCopy := *b.liveReview
	reviewCopy.Comments = slices.Clone(b.liveReview.Comments)
	reviewCopy.Threads = cloneFakeReviewThreads(b.liveReview.Threads)
	return reviewCopy
}

func (b *fakePendingReviewBackend) operations() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.requestedOps)
}

func (b *fakePendingReviewBackend) hasReview() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.liveReview != nil
}

func (b *fakePendingReviewBackend) graphQLComments() []map[string]any {
	comments := make([]map[string]any, 0, len(b.liveReview.Comments))
	for _, comment := range b.liveReview.Comments {
		comments = append(comments, b.graphQLComment(comment))
	}
	return comments
}

func (b *fakePendingReviewBackend) graphQLThreads() []map[string]any {
	if b.liveReview == nil {
		return nil
	}

	commentsByThread := make(map[string][]map[string]any)
	threadIDs := make([]string, 0)
	for _, thread := range b.liveReview.Threads {
		threadIDs = append(threadIDs, thread.ID)
		for _, comment := range thread.Comments {
			commentsByThread[thread.ID] = append(commentsByThread[thread.ID], b.graphQLComment(comment))
		}
	}
	for _, comment := range b.liveReview.Comments {
		if comment.ThreadID == "" {
			continue
		}
		if _, exists := commentsByThread[comment.ThreadID]; !exists {
			threadIDs = append(threadIDs, comment.ThreadID)
		}
		commentsByThread[comment.ThreadID] = append(commentsByThread[comment.ThreadID], b.graphQLComment(comment))
	}

	threads := make([]map[string]any, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		threads = append(threads, map[string]any{
			"id": threadID,
			"comments": map[string]any{
				"nodes": commentsByThread[threadID],
			},
		})
	}
	return threads
}

func (b *fakePendingReviewBackend) graphQLComment(comment fakeReviewComment) map[string]any {
	replyTo := any(nil)
	if comment.ReplyToCommentID != "" {
		replyTo = map[string]any{"id": comment.ReplyToCommentID}
	}

	return map[string]any{
		"id":            comment.ID,
		"threadID":      comment.ThreadID,
		"body":          comment.Body,
		"path":          comment.Path,
		"line":          comment.Line,
		"diffSide":      comment.Side,
		"startLine":     comment.StartLine,
		"startDiffSide": comment.StartSide,
		"author": map[string]any{
			"login": b.commentAuthorLogin(comment),
		},
		"replyTo": replyTo,
		"pullRequestReview": map[string]any{
			"id": b.liveReview.ID,
		},
	}
}

func (b *fakePendingReviewBackend) deleteComment(commentID string) bool {
	for i, comment := range b.liveReview.Comments {
		if comment.ID != commentID {
			continue
		}
		b.liveReview.Comments = append(b.liveReview.Comments[:i], b.liveReview.Comments[i+1:]...)
		return true
	}
	return false
}

func (b *fakePendingReviewBackend) addThreadComment(input map[string]any) fakeReviewComment {
	comment := fakeReviewComment{
		ID:       b.nextGraphQLID("comment"),
		ThreadID: b.nextGraphQLID("thread"),
		Body:     stringValue(input["body"]),
		Path:     stringValue(input["path"]),
		Line:     intValue(input["line"]),
		Side:     stringValue(input["side"]),
	}
	comment.StartLine = intValue(input["startLine"])
	comment.StartSide = stringValue(input["startSide"])

	b.liveReview.Comments = append(b.liveReview.Comments, comment)
	return comment
}

func (b *fakePendingReviewBackend) addThreadReplyComment(threadID string, body string) (fakeReviewComment, error) {
	threadComments := b.liveReview.commentsForThread(threadID)
	if len(threadComments) == 0 {
		return fakeReviewComment{}, fmt.Errorf("thread %q not found", threadID)
	}

	parent := threadComments[len(threadComments)-1]
	comment := fakeReviewComment{
		ID:               b.nextGraphQLID("comment"),
		ThreadID:         threadID,
		ReplyToCommentID: parent.ID,
		Body:             body,
		Path:             parent.Path,
		Line:             parent.Line,
		Side:             parent.Side,
		StartLine:        parent.StartLine,
		StartSide:        parent.StartSide,
		AuthorLogin:      b.viewer,
	}
	b.liveReview.Comments = append(b.liveReview.Comments, comment)
	return comment, nil
}

func (b *fakePendingReviewBackend) commentAuthorLogin(comment fakeReviewComment) string {
	if comment.AuthorLogin != "" {
		return comment.AuthorLogin
	}
	return b.liveReview.Author.Login
}

func (b *fakePendingReviewBackend) nextGraphQLID(prefix string) string {
	id := fmt.Sprintf("%s-%d", prefix, b.nextID)
	b.nextID++
	return id
}

func (b *fakePendingReviewBackend) advanceNextID(id string) {
	var prefix string
	var value int
	if _, err := fmt.Sscanf(id, "%[^-]-%d", &prefix, &value); err != nil {
		return
	}
	if value >= b.nextID {
		b.nextID = value + 1
	}
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func (r fakeReview) commentsForThread(threadID string) []fakeReviewComment {
	comments := make([]fakeReviewComment, 0)
	for _, thread := range r.Threads {
		if thread.ID == threadID {
			comments = append(comments, thread.Comments...)
		}
	}
	for _, comment := range r.Comments {
		if comment.ThreadID == threadID {
			comments = append(comments, comment)
		}
	}
	return comments
}

func cloneFakeReviewThreads(threads []fakeReviewThread) []fakeReviewThread {
	cloned := make([]fakeReviewThread, 0, len(threads))
	for _, thread := range threads {
		cloned = append(cloned, fakeReviewThread{
			ID:       thread.ID,
			Comments: slices.Clone(thread.Comments),
		})
	}
	return cloned
}
