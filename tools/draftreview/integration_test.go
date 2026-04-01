// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package draftreview

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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

	reviews, err := client.ListReviews(ctx, repo, prNumber)
	require.NoError(t, err)

	if review, found := FindPendingReviewForAuthor(reviews, viewer.Login); found {
		require.Equalf(t, "1", envOrFallback("GH_PENDING_REVIEW_TEST_DELETE_EXISTING", "GH_DRAFT_REVIEW_TEST_DELETE_EXISTING"), "refusing to create a new pending review because %s already has pending review %d; set GH_PENDING_REVIEW_TEST_DELETE_EXISTING=1 to delete it first", viewer.Login, review.ID)
		require.NoError(t, client.DeletePendingReview(ctx, repo, prNumber, review.ID))
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

	reviews, err = client.ListReviews(ctx, repo, prNumber)
	require.NoError(t, err)
	review, found := FindPendingReviewForAuthor(reviews, viewer.Login)
	require.True(t, found, "expected pending review for %s after create; stdout=%q", viewer.Login, stdout.String())

	t.Cleanup(func() {
		require.NoError(t, client.DeletePendingReview(t.Context(), repo, prNumber, review.ID))
	})

	fetched, err := client.GetReview(ctx, repo, prNumber, review.ID)
	require.NoError(t, err)
	require.Equal(t, viewer.Login, fetched.User.Login)
	require.Equal(t, reviewStatePending, fetched.State)
	require.Equal(t, "test", fetched.Body)

	_, err = client.UpdatePendingReviewBody(ctx, repo, prNumber, review.ID, "user changed this")
	require.NoError(t, err)

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

	fetched, err = client.GetReview(ctx, repo, prNumber, review.ID)
	require.NoError(t, err)
	require.Equal(t, "agent reconciled result", fetched.Body)
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
		require.NoError(t, deleteCurrentPendingReview(ctx, client, repo, prNumber, viewer.Login))
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

	review, err := getCurrentPendingReviewWithComments(ctx, client, repo, prNumber, viewer.Login)
	require.NoError(t, err)
	require.Equal(t, "test", review.Body)
	require.Equal(t, normalizeDraftReviewComments(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login))

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
	require.Equal(t, normalizeDraftReviewComments(desiredComments), normalizeLiveReviewComments(fetched.Comments, viewer.Login))

	externalComments := []DraftReviewComment{{
		Path: commentPath,
		Line: commentLine,
		Side: commentSide,
		Body: "user changed this comment",
	}}
	require.NoError(t, client.DeletePendingReview(ctx, repo, prNumber, review.ID))
	_, err = client.CreatePendingReviewWithComments(ctx, repo, prNumber, review.Body, externalComments)
	require.NoError(t, err)

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
	require.Equal(t, normalizeDraftReviewComments(desiredComments), normalizeLiveReviewComments(review.Comments, viewer.Login))
}

func envOrFallback(primary string, fallback string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(fallback)
}

func getCurrentPendingReviewWithComments(ctx context.Context, client *GitHubClient, repo string, prNumber int, login string) (Review, error) {
	reviews, err := client.ListReviews(ctx, repo, prNumber)
	if err != nil {
		return Review{}, err
	}

	review, found := FindPendingReviewForAuthor(reviews, login)
	if !found {
		return Review{}, stacktrace.Errorf("no pending review found for %s on %s#%d", login, repo, prNumber)
	}

	comments, err := client.ListReviewComments(ctx, repo, prNumber, review.ID)
	if err != nil {
		return Review{}, err
	}
	review.Comments = comments
	return review, nil
}

func ensureNoLivePendingReviewOrDelete(ctx context.Context, t *testing.T, client *GitHubClient, repo string, prNumber int, login string, deleteExisting bool) error {
	t.Helper()

	reviews, err := client.ListReviews(ctx, repo, prNumber)
	if err != nil {
		return err
	}

	review, found := FindPendingReviewForAuthor(reviews, login)
	if !found {
		return nil
	}
	if !deleteExisting {
		return stacktrace.Errorf("refusing to create a new pending review because %s already has pending review %d; set GH_PENDING_REVIEW_TEST_DELETE_EXISTING=1 to delete it first", login, review.ID)
	}
	return client.DeletePendingReview(ctx, repo, prNumber, review.ID)
}

func deleteCurrentPendingReview(ctx context.Context, client *GitHubClient, repo string, prNumber int, login string) error {
	reviews, err := client.ListReviews(ctx, repo, prNumber)
	if err != nil {
		return err
	}

	review, found := FindPendingReviewForAuthor(reviews, login)
	if !found {
		return nil
	}
	return client.DeletePendingReview(ctx, repo, prNumber, review.ID)
}

func writeDraftReviewCommentsFile(path string, comments []DraftReviewComment) error {
	content, err := json.Marshal(comments)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
