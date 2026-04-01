// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package draftreview

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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

func envOrFallback(primary string, fallback string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(fallback)
}
