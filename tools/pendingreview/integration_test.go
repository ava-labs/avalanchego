// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestCreatePendingReviewLive(t *testing.T) {
	if envOrFallback("GH_PENDING_REVIEW_LIVE_TEST", "GH_DRAFT_REVIEW_LIVE_TEST") != "1" {
		t.Skip("set GH_PENDING_REVIEW_LIVE_TEST=1 to run live GitHub integration test")
	}

	prValue := envOrFallback("GH_PENDING_REVIEW_TEST_PR", "GH_DRAFT_REVIEW_TEST_PR")
	require.NotEmpty(t, prValue, "GH_PENDING_REVIEW_TEST_PR is required")
	prNumber, err := strconv.Atoi(prValue)
	require.NoError(t, err)
	require.Positive(t, prNumber, "invalid GH_PENDING_REVIEW_TEST_PR %q", prValue)

	repo := envOrFallback("GH_PENDING_REVIEW_TEST_REPO", "GH_DRAFT_REVIEW_TEST_REPO")
	if repo == "" {
		repo = defaultRepo
	}

	configDir := envOrFallback("GH_PENDING_REVIEW_CONFIG_DIR", "GH_DRAFT_REVIEW_CONFIG_DIR")
	if configDir == "" {
		configDir = defaultConfigDir()
	}
	stateDir := t.TempDir()

	ctx := t.Context()
	tokenProvider := NewGHTokenProvider(logging.NoLog{})
	token, err := tokenProvider.Token(ctx, configDir)
	require.NoError(t, err)

	client := NewGitHubClient(logging.NoLog{}, DefaultHTTPClient(), defaultGitHubAPIBaseURL, token)
	viewer, err := client.Viewer(ctx)
	require.NoError(t, err)

	if review, err := client.GetPendingReview(ctx, repo, prNumber, viewer.Login); err == nil {
		require.Equalf(t, "1", envOrFallback("GH_PENDING_REVIEW_TEST_DELETE_EXISTING", "GH_DRAFT_REVIEW_TEST_DELETE_EXISTING"), "refusing to create a new pending review because %s already has pending review %s; set GH_PENDING_REVIEW_TEST_DELETE_EXISTING=1 to delete it first", viewer.Login, displayReviewID(review))
		require.NoError(t, client.DeletePendingReview(ctx, review.ID))
	}

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = tokenProvider

	require.NoError(t, app.Run(ctx, []string{
		"create",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "test",
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))

	review, err := client.GetPendingReview(ctx, repo, prNumber, viewer.Login)
	require.NoError(t, err, "expected pending review for %s after create; stdout=%q", viewer.Login, stdout.String())

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		require.NoError(t, client.DeletePendingReview(cleanupCtx, review.ID))
	})

	fetched, err := client.GetPendingReview(ctx, repo, prNumber, viewer.Login)
	require.NoError(t, err)
	require.Equal(t, viewer.Login, fetched.User.Login)
	require.Equal(t, reviewStatePending, fetched.State)
	require.Equal(t, "test", fetched.Body)

	_, err = client.UpdatePendingReviewBody(ctx, review.ID, "user changed this")
	require.NoError(t, err)
	require.NoError(t, waitForPendingReview(ctx, client, repo, prNumber, viewer.Login, func(review Review) bool {
		return review.Body == "user changed this"
	}))

	err = app.Run(ctx, []string{
		"update-body",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "agent overwrite",
		"--config-dir", configDir,
		"--state-dir", stateDir,
	})
	require.ErrorIs(t, err, ErrReviewConflict)

	require.NoError(t, app.Run(ctx, []string{
		"update-body",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "agent reconciled result",
		"--config-dir", configDir,
		"--state-dir", stateDir,
		"--force",
	}))

	fetched, err = client.GetPendingReview(ctx, repo, prNumber, viewer.Login)
	require.NoError(t, err)
	require.Equal(t, "agent reconciled result", fetched.Body)
}

func TestCreateDeletePendingReviewProxyLive(t *testing.T) {
	if os.Getenv("GH_PENDING_REVIEW_PROXY_LIVE_TEST") != "1" {
		t.Skip("set GH_PENDING_REVIEW_PROXY_LIVE_TEST=1 to run live pending-review proxy integration test")
	}

	prValue := envOrFallback("GH_PENDING_REVIEW_TEST_PR", "GH_DRAFT_REVIEW_TEST_PR")
	require.NotEmpty(t, prValue, "GH_PENDING_REVIEW_TEST_PR is required")
	prNumber, err := strconv.Atoi(prValue)
	require.NoError(t, err)
	require.Positive(t, prNumber, "invalid GH_PENDING_REVIEW_TEST_PR %q", prValue)

	repo := envOrFallback("GH_PENDING_REVIEW_TEST_REPO", "GH_DRAFT_REVIEW_TEST_REPO")
	if repo == "" {
		repo = defaultRepo
	}

	configDir := envOrFallback("GH_PENDING_REVIEW_CONFIG_DIR", "GH_DRAFT_REVIEW_CONFIG_DIR")
	if configDir == "" {
		configDir = defaultConfigDir()
	}
	stateDir := t.TempDir()

	ctx := t.Context()
	tokenProvider := NewGHTokenProvider(logging.NoLog{})
	token, err := tokenProvider.Token(ctx, configDir)
	require.NoError(t, err)

	client := NewGitHubClient(logging.NoLog{}, DefaultHTTPClient(), defaultGitHubAPIBaseURL, token)
	viewer, err := client.Viewer(ctx)
	require.NoError(t, err)

	deleteExisting := envOrFallback("GH_PENDING_REVIEW_TEST_DELETE_EXISTING", "GH_DRAFT_REVIEW_TEST_DELETE_EXISTING")
	require.NoError(t, ensureNoLivePendingReviewOrDelete(ctx, t, client, repo, prNumber, viewer.Login, deleteExisting == "1"))

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		require.NoError(t, deleteCurrentPendingReview(cleanupCtx, client, repo, prNumber, viewer.Login))
		_, err := client.GetPendingReview(cleanupCtx, repo, prNumber, viewer.Login)
		require.ErrorIs(t, err, ErrNoPendingReview)
	})

	t.Setenv(proxyAllowedRepoVarName, repo)
	server := httptest.NewServer(NewProxyHandler())
	defer server.Close()
	t.Setenv(proxyURLVarName, server.URL)

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = failTokenProvider{t: t}

	require.NoError(t, app.Run(ctx, []string{
		"create",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "proxy live test",
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))

	review, err := client.GetPendingReview(ctx, repo, prNumber, viewer.Login)
	require.NoError(t, err, "expected pending review for %s after proxy create; stdout=%q", viewer.Login, stdout.String())
	require.Equal(t, "proxy live test", review.Body)

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{
		"delete",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--config-dir", configDir,
		"--state-dir", stateDir,
		"--ensure-absent",
	}))

	_, err = client.GetPendingReview(ctx, repo, prNumber, viewer.Login)
	require.ErrorIs(t, err, ErrNoPendingReview)
}

func TestReplacePendingReviewCommentsLive(t *testing.T) {
	if envOrFallback("GH_PENDING_REVIEW_LIVE_TEST", "GH_DRAFT_REVIEW_LIVE_TEST") != "1" {
		t.Skip("set GH_PENDING_REVIEW_LIVE_TEST=1 to run live GitHub integration test")
	}

	prValue := envOrFallback("GH_PENDING_REVIEW_TEST_PR", "GH_DRAFT_REVIEW_TEST_PR")
	require.NotEmpty(t, prValue, "GH_PENDING_REVIEW_TEST_PR is required")
	prNumber, err := strconv.Atoi(prValue)
	require.NoError(t, err)
	require.Positive(t, prNumber, "invalid GH_PENDING_REVIEW_TEST_PR %q", prValue)

	repo := envOrFallback("GH_PENDING_REVIEW_TEST_REPO", "GH_DRAFT_REVIEW_TEST_REPO")
	if repo == "" {
		repo = defaultRepo
	}

	configDir := envOrFallback("GH_PENDING_REVIEW_CONFIG_DIR", "GH_DRAFT_REVIEW_CONFIG_DIR")
	if configDir == "" {
		configDir = defaultConfigDir()
	}

	commentPath := envOrFallback("GH_PENDING_REVIEW_TEST_COMMENT_PATH", "GH_DRAFT_REVIEW_TEST_COMMENT_PATH")
	require.NotEmpty(t, commentPath, "GH_PENDING_REVIEW_TEST_COMMENT_PATH is required")
	commentLineValue := envOrFallback("GH_PENDING_REVIEW_TEST_COMMENT_LINE", "GH_DRAFT_REVIEW_TEST_COMMENT_LINE")
	require.NotEmpty(t, commentLineValue, "GH_PENDING_REVIEW_TEST_COMMENT_LINE is required")
	commentLine, err := strconv.Atoi(commentLineValue)
	require.NoError(t, err)
	require.Positive(t, commentLine, "invalid GH_PENDING_REVIEW_TEST_COMMENT_LINE %q", commentLineValue)
	commentSide := envOrFallback("GH_PENDING_REVIEW_TEST_COMMENT_SIDE", "GH_DRAFT_REVIEW_TEST_COMMENT_SIDE")
	if commentSide == "" {
		commentSide = "RIGHT"
	}

	stateDir := t.TempDir()
	ctx := t.Context()
	tokenProvider := NewGHTokenProvider(logging.NoLog{})
	token, err := tokenProvider.Token(ctx, configDir)
	require.NoError(t, err)

	client := NewGitHubClient(logging.NoLog{}, DefaultHTTPClient(), defaultGitHubAPIBaseURL, token)
	viewer, err := client.Viewer(ctx)
	require.NoError(t, err)

	deleteExisting := envOrFallback("GH_PENDING_REVIEW_TEST_DELETE_EXISTING", "GH_DRAFT_REVIEW_TEST_DELETE_EXISTING")
	require.NoError(t, ensureNoLivePendingReviewOrDelete(ctx, t, client, repo, prNumber, viewer.Login, deleteExisting == "1"))

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		require.NoError(t, deleteCurrentPendingReview(cleanupCtx, client, repo, prNumber, viewer.Login))
	})

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.tokenProvider = tokenProvider

	require.NoError(t, app.Run(ctx, []string{
		"create",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "test",
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))

	desiredCommentsPath := filepath.Join(t.TempDir(), "desired-comments.json")
	desiredComments := []DraftReviewComment{{
		Path: commentPath,
		Line: commentLine,
		Side: commentSide,
		Body: "agent comment",
	}}
	require.NoError(t, writeDraftReviewCommentsFile(desiredCommentsPath, desiredComments))

	require.NoError(t, app.Run(ctx, []string{
		"replace-comments",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--comments-file", desiredCommentsPath,
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))
	require.NoError(t, waitForPendingReview(ctx, client, repo, prNumber, viewer.Login, func(review Review) bool {
		return draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login))
	}))

	review, err := getCurrentPendingReviewWithComments(ctx, client, repo, prNumber, viewer.Login)
	require.NoError(t, err)
	require.Equal(t, "test", review.Body)
	require.True(t, draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login)))

	var stdout bytes.Buffer
	app.Stdout = &stdout
	require.NoError(t, app.Run(ctx, []string{
		"get",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))

	var fetched Review
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &fetched))
	require.Equal(t, "test", fetched.Body)
	require.True(t, draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(fetched.Comments, viewer.Login)))

	externalComments := []DraftReviewComment{{
		Path: commentPath,
		Line: commentLine,
		Side: commentSide,
		Body: "user changed this comment",
	}}
	require.NoError(t, client.ReplacePendingReviewEntries(ctx, review.ID, review.Comments, externalComments))
	require.NoError(t, waitForPendingReview(ctx, client, repo, prNumber, viewer.Login, func(review Review) bool {
		return draftReviewEntriesEqual(normalizeDraftReviewEntries(externalComments), normalizeLiveReviewComments(review.Comments, viewer.Login))
	}))

	err = app.Run(ctx, []string{
		"replace-comments",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--comments-file", desiredCommentsPath,
		"--config-dir", configDir,
		"--state-dir", stateDir,
	})
	require.ErrorIs(t, err, ErrReviewCommentsConflict)

	require.NoError(t, app.Run(ctx, []string{
		"replace-comments",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--comments-file", desiredCommentsPath,
		"--config-dir", configDir,
		"--state-dir", stateDir,
		"--force",
	}))
	require.NoError(t, waitForPendingReview(ctx, client, repo, prNumber, viewer.Login, func(review Review) bool {
		return draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login))
	}))

	review, err = getCurrentPendingReviewWithComments(ctx, client, repo, prNumber, viewer.Login)
	require.NoError(t, err)
	require.Equal(t, "test", review.Body)
	require.True(t, draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login)))
}

func TestReplacePendingReviewThreadRepliesLive(t *testing.T) {
	if envOrFallback("GH_PENDING_REVIEW_LIVE_TEST", "GH_DRAFT_REVIEW_LIVE_TEST") != "1" {
		t.Skip("set GH_PENDING_REVIEW_LIVE_TEST=1 to run live GitHub integration test")
	}

	prValue := envOrFallback("GH_PENDING_REVIEW_TEST_PR", "GH_DRAFT_REVIEW_TEST_PR")
	require.NotEmpty(t, prValue, "GH_PENDING_REVIEW_TEST_PR is required")
	prNumber, err := strconv.Atoi(prValue)
	require.NoError(t, err)
	require.Positive(t, prNumber, "invalid GH_PENDING_REVIEW_TEST_PR %q", prValue)

	repo := envOrFallback("GH_PENDING_REVIEW_TEST_REPO", "GH_DRAFT_REVIEW_TEST_REPO")
	if repo == "" {
		repo = defaultRepo
	}

	configDir := envOrFallback("GH_PENDING_REVIEW_CONFIG_DIR", "GH_DRAFT_REVIEW_CONFIG_DIR")
	if configDir == "" {
		configDir = defaultConfigDir()
	}

	replyThreadPath := envOrFallback("GH_PENDING_REVIEW_TEST_REPLY_THREAD_PATH", "")
	if replyThreadPath == "" {
		replyThreadPath = ".github/workflows/bazel-ci.yml"
	}

	stateDir := t.TempDir()
	ctx := t.Context()
	tokenProvider := NewGHTokenProvider(logging.NoLog{})
	token, err := tokenProvider.Token(ctx, configDir)
	require.NoError(t, err)

	client := NewGitHubClient(logging.NoLog{}, DefaultHTTPClient(), defaultGitHubAPIBaseURL, token)
	viewer, err := client.Viewer(ctx)
	require.NoError(t, err)

	deleteExisting := envOrFallback("GH_PENDING_REVIEW_TEST_DELETE_EXISTING", "GH_DRAFT_REVIEW_TEST_DELETE_EXISTING")
	require.NoError(t, ensureNoLivePendingReviewOrDelete(ctx, t, client, repo, prNumber, viewer.Login, deleteExisting == "1"))

	threadID, err := findExistingReviewThreadID(ctx, client, repo, prNumber, envOrFallback("GH_PENDING_REVIEW_TEST_REPLY_THREAD_ID", ""), replyThreadPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		require.NoError(t, deleteCurrentPendingReview(cleanupCtx, client, repo, prNumber, viewer.Login))
		_, err := client.GetPendingReview(cleanupCtx, repo, prNumber, viewer.Login)
		require.ErrorIs(t, err, ErrNoPendingReview)
	})

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.tokenProvider = tokenProvider

	require.NoError(t, app.Run(ctx, []string{
		"create",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "test",
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))

	desiredCommentsPath := filepath.Join(t.TempDir(), "desired-thread-replies.json")
	desiredComments := []DraftReviewComment{{
		Kind:     DraftReviewEntryKindThreadReply,
		ThreadID: threadID,
		Body:     "agent reply",
	}}
	require.NoError(t, writeDraftReviewCommentsFile(desiredCommentsPath, desiredComments))

	require.NoError(t, app.Run(ctx, []string{
		"replace-comments",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--comments-file", desiredCommentsPath,
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))

	review, err := getCurrentPendingReviewWithComments(ctx, client, repo, prNumber, viewer.Login)
	require.NoError(t, err)
	require.Equal(t, "test", review.Body)
	require.True(t, draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login)))
	require.True(t, hasReviewComment(review.Comments, func(comment ReviewComment) bool {
		return comment.ThreadID == threadID &&
			comment.Kind == DraftReviewEntryKindThreadReply &&
			comment.Body == "agent reply" &&
			comment.User.Login == viewer.Login
	}))

	var stdout bytes.Buffer
	app.Stdout = &stdout
	require.NoError(t, app.Run(ctx, []string{
		"get",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}))

	var fetched Review
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &fetched))
	require.Equal(t, "test", fetched.Body)
	require.True(t, draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(fetched.Comments, viewer.Login)))

	externalComments := []DraftReviewComment{{
		Kind:     DraftReviewEntryKindThreadReply,
		ThreadID: threadID,
		Body:     "user changed this reply",
	}}
	require.NoError(t, client.ReplacePendingReviewEntries(ctx, review.ID, review.Comments, externalComments))
	require.NoError(t, waitForPendingReview(ctx, client, repo, prNumber, viewer.Login, func(review Review) bool {
		return draftReviewEntriesEqual(normalizeDraftReviewEntries(externalComments), normalizeLiveReviewComments(review.Comments, viewer.Login))
	}))

	err = app.Run(ctx, []string{
		"replace-comments",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--comments-file", desiredCommentsPath,
		"--config-dir", configDir,
		"--state-dir", stateDir,
	})
	require.ErrorIs(t, err, ErrReviewCommentsConflict)

	require.NoError(t, app.Run(ctx, []string{
		"replace-comments",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--comments-file", desiredCommentsPath,
		"--config-dir", configDir,
		"--state-dir", stateDir,
		"--force",
	}))

	review, err = getCurrentPendingReviewWithComments(ctx, client, repo, prNumber, viewer.Login)
	require.NoError(t, err)
	require.Equal(t, "test", review.Body)
	require.True(t, draftReviewEntriesEqual(normalizeDraftReviewEntries(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login)))
}

func envOrFallback(primary string, fallback string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(fallback)
}

func findExistingReviewThreadID(ctx context.Context, client *GitHubClient, repo string, prNumber int, explicitThreadID string, preferredPath string) (string, error) {
	if explicitThreadID != "" {
		return explicitThreadID, nil
	}

	owner, name, err := splitRepo(repo)
	if err != nil {
		return "", err
	}

	var resp struct {
		Repository struct {
			PullRequest *struct {
				ReviewThreads struct {
					Nodes []struct {
						ID       string `json:"id"`
						Comments struct {
							Nodes []struct {
								Path    string `json:"path"`
								ReplyTo *struct {
									ID string `json:"id"`
								} `json:"replyTo"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := client.graphql(ctx, `
query ExistingReviewThreads($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) {
        nodes {
          id
          comments(first: 100) {
            nodes {
              path
              replyTo { id }
            }
          }
        }
      }
    }
  }
}`, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": prNumber,
	}, &resp); err != nil {
		return "", err
	}

	if resp.Repository.PullRequest == nil {
		return "", stacktrace.Errorf("pull request not found for %s#%d", repo, prNumber)
	}

	var fallbackThreadID string
	for _, thread := range resp.Repository.PullRequest.ReviewThreads.Nodes {
		if thread.ID == "" {
			continue
		}
		hasRootComment := false
		matchesPreferredPath := false
		for _, comment := range thread.Comments.Nodes {
			if comment.ReplyTo == nil {
				hasRootComment = true
			}
			if preferredPath != "" && comment.Path == preferredPath {
				matchesPreferredPath = true
			}
		}
		if !hasRootComment {
			continue
		}
		if matchesPreferredPath {
			return thread.ID, nil
		}
		if fallbackThreadID == "" {
			fallbackThreadID = thread.ID
		}
	}

	if fallbackThreadID != "" {
		return fallbackThreadID, nil
	}
	return "", stacktrace.Errorf("no existing review thread found on %s#%d", repo, prNumber)
}

func getCurrentPendingReviewWithComments(ctx context.Context, client *GitHubClient, repo string, prNumber int, login string) (Review, error) {
	return client.GetPendingReview(ctx, repo, prNumber, login)
}

func hasReviewComment(comments []ReviewComment, predicate func(ReviewComment) bool) bool {
	for _, comment := range comments {
		if predicate(comment) {
			return true
		}
	}
	return false
}

func ensureNoLivePendingReviewOrDelete(ctx context.Context, t *testing.T, client *GitHubClient, repo string, prNumber int, login string, deleteExisting bool) error {
	t.Helper()

	review, err := client.GetPendingReview(ctx, repo, prNumber, login)
	if err != nil {
		if errors.Is(err, ErrNoPendingReview) {
			return nil
		}
		return err
	}
	if !deleteExisting {
		return stacktrace.Errorf("refusing to create a new pending review because %s already has pending review %s; set GH_PENDING_REVIEW_TEST_DELETE_EXISTING=1 to delete it first", login, displayReviewID(review))
	}
	return client.DeletePendingReview(ctx, review.ID)
}

func deleteCurrentPendingReview(ctx context.Context, client *GitHubClient, repo string, prNumber int, login string) error {
	review, err := client.GetPendingReview(ctx, repo, prNumber, login)
	if err != nil {
		if errors.Is(err, ErrNoPendingReview) {
			return nil
		}
		return err
	}
	return client.DeletePendingReview(ctx, review.ID)
}

func writeDraftReviewCommentsFile(path string, comments []DraftReviewComment) error {
	content, err := json.Marshal(comments)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func waitForPendingReview(ctx context.Context, client *GitHubClient, repo string, prNumber int, login string, predicate func(Review) bool) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		review, err := client.GetPendingReview(ctx, repo, prNumber, login)
		if err == nil && predicate(review) {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return stacktrace.Errorf("pending review on %s#%d did not reach expected state before timeout", repo, prNumber)
		}

		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
