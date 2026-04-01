package draftreview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCreatePendingReviewLive(t *testing.T) {
	if os.Getenv("GH_DRAFT_REVIEW_LIVE_TEST") != "1" {
		t.Skip("set GH_DRAFT_REVIEW_LIVE_TEST=1 to run live GitHub integration test")
	}

	prValue := os.Getenv("GH_DRAFT_REVIEW_TEST_PR")
	if prValue == "" {
		t.Fatal("GH_DRAFT_REVIEW_TEST_PR is required")
	}
	prNumber, err := strconv.Atoi(prValue)
	if err != nil || prNumber <= 0 {
		t.Fatalf("invalid GH_DRAFT_REVIEW_TEST_PR %q", prValue)
	}

	repo := os.Getenv("GH_DRAFT_REVIEW_TEST_REPO")
	if repo == "" {
		repo = defaultRepo
	}

	configDir := os.Getenv("GH_DRAFT_REVIEW_CONFIG_DIR")
	if configDir == "" {
		configDir = defaultConfigDir()
	}
	stateDir := t.TempDir()

	ctx := context.Background()
	tokenProvider := NewGHTokenProvider()
	token, err := tokenProvider.Token(ctx, configDir)
	if err != nil {
		t.Fatalf("acquire token: %v", err)
	}

	client := NewGitHubClient(DefaultHTTPClient(), defaultGitHubAPIBaseURL, token)
	viewer, err := client.Viewer(ctx)
	if err != nil {
		t.Fatalf("resolve viewer: %v", err)
	}

	reviews, err := client.ListReviews(ctx, repo, prNumber)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}

	if review, found := FindPendingReviewForAuthor(reviews, viewer.Login); found {
		if os.Getenv("GH_DRAFT_REVIEW_TEST_DELETE_EXISTING") != "1" {
			t.Fatalf("refusing to create a new pending review because %s already has pending review %d; set GH_DRAFT_REVIEW_TEST_DELETE_EXISTING=1 to delete it first", viewer.Login, review.ID)
		}
		if err := client.DeletePendingReview(ctx, repo, prNumber, review.ID); err != nil {
			t.Fatalf("delete pre-existing pending review %d: %v", review.ID, err)
		}
	}

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = tokenProvider

	if err := app.Run(ctx, []string{
		"create",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "test",
		"--config-dir", configDir,
		"--state-dir", stateDir,
	}); err != nil {
		t.Fatalf("run create command: %v", err)
	}

	reviews, err = client.ListReviews(ctx, repo, prNumber)
	if err != nil {
		t.Fatalf("list reviews after create: %v", err)
	}
	review, found := FindPendingReviewForAuthor(reviews, viewer.Login)
	if !found {
		t.Fatalf("expected pending review for %s after create; stdout=%q", viewer.Login, stdout.String())
	}

	t.Cleanup(func() {
		if err := client.DeletePendingReview(context.Background(), repo, prNumber, review.ID); err != nil {
			t.Fatalf("delete pending review %d: %v", review.ID, err)
		}
	})

	fetched, err := client.GetReview(ctx, repo, prNumber, review.ID)
	if err != nil {
		t.Fatalf("get review: %v", err)
	}
	if fetched.User.Login != viewer.Login {
		t.Fatalf("unexpected review author %q", fetched.User.Login)
	}
	if fetched.State != reviewStatePending {
		t.Fatalf("unexpected review state %q", fetched.State)
	}
	if fetched.Body != "test" {
		t.Fatalf("unexpected review body %q", fetched.Body)
	}

	if _, err := client.UpdatePendingReviewBody(ctx, repo, prNumber, review.ID, "user changed this"); err != nil {
		t.Fatalf("simulate external update: %v", err)
	}

	err = app.Run(ctx, []string{
		"update-body",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "agent overwrite",
		"--config-dir", configDir,
		"--state-dir", stateDir,
	})
	if !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("expected ErrReviewConflict after external change, got: %v", err)
	}

	if err := app.Run(ctx, []string{
		"update-body",
		"--repo", repo,
		"--pr", strconv.Itoa(prNumber),
		"--body", "agent reconciled result",
		"--config-dir", configDir,
		"--state-dir", stateDir,
		"--force",
	}); err != nil {
		t.Fatalf("force update after conflict: %v", err)
	}

	fetched, err = client.GetReview(ctx, repo, prNumber, review.ID)
	if err != nil {
		t.Fatalf("get review after force update: %v", err)
	}
	if fetched.Body != "agent reconciled result" {
		t.Fatalf("unexpected review body after force update %q", fetched.Body)
	}
}
