package draftreview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

type GitHubClient struct {
	httpClient HTTPDoer
	baseURL    string
	token      string
	log        logging.Logger
}

func NewGitHubClient(log logging.Logger, httpClient HTTPDoer, baseURL string, token string) *GitHubClient {
	return &GitHubClient{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		log:        log,
	}
}

func (c *GitHubClient) Viewer(ctx context.Context) (User, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return User{}, stacktrace.Wrap(err)
	}

	var user User
	if err := c.doJSON(req, &user); err != nil {
		return User{}, stacktrace.Wrap(err)
	}
	return user, nil
}

func (c *GitHubClient) CreatePendingReview(ctx context.Context, repo string, prNumber int, body string) (Review, error) {
	return c.CreatePendingReviewWithComments(ctx, repo, prNumber, body, nil)
}

func (c *GitHubClient) CreatePendingReviewWithComments(ctx context.Context, repo string, prNumber int, body string, comments []DraftReviewComment) (Review, error) {
	payload := struct {
		Body     string           `json:"body"`
		Comments []map[string]any `json:"comments,omitempty"`
	}{
		Body:     body,
		Comments: marshalCommentsForCreate(comments),
	}

	req, err := c.newRequest(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, prNumber), payload)
	if err != nil {
		return Review{}, stacktrace.Wrap(err)
	}

	var review Review
	if err := c.doJSON(req, &review); err != nil {
		return Review{}, stacktrace.Wrap(err)
	}
	return review, nil
}

func (c *GitHubClient) GetReview(ctx context.Context, repo string, prNumber int, reviewID int64) (Review, error) {
	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/reviews/%d", repo, prNumber, reviewID), nil)
	if err != nil {
		return Review{}, stacktrace.Wrap(err)
	}

	var review Review
	if err := c.doJSON(req, &review); err != nil {
		return Review{}, stacktrace.Wrap(err)
	}
	return review, nil
}

func (c *GitHubClient) ListReviews(ctx context.Context, repo string, prNumber int) ([]Review, error) {
	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, prNumber), nil)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	var reviews []Review
	if err := c.doJSON(req, &reviews); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return reviews, nil
}

func (c *GitHubClient) ListReviewComments(ctx context.Context, repo string, prNumber int, reviewID int64) ([]ReviewComment, error) {
	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/reviews/%d/comments", repo, prNumber, reviewID), nil)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	var comments []ReviewComment
	if err := c.doJSON(req, &comments); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return comments, nil
}

func (c *GitHubClient) DeletePendingReview(ctx context.Context, repo string, prNumber int, reviewID int64) error {
	req, err := c.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/pulls/%d/reviews/%d", repo, prNumber, reviewID), nil)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	c.log.Debug("sending GitHub API request",
		zap.String("method", req.Method),
		zap.String("path", req.URL.Path),
	)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer resp.Body.Close()

	c.log.Debug("received GitHub API response",
		zap.String("method", req.Method),
		zap.String("path", req.URL.Path),
		zap.Int("statusCode", resp.StatusCode),
	)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return stacktrace.Errorf("GitHub API %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *GitHubClient) UpdatePendingReviewBody(ctx context.Context, repo string, prNumber int, reviewID int64, body string) (Review, error) {
	payload := struct {
		Body string `json:"body"`
	}{
		Body: body,
	}

	req, err := c.newRequest(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/pulls/%d/reviews/%d", repo, prNumber, reviewID), payload)
	if err != nil {
		return Review{}, stacktrace.Wrap(err)
	}

	var review Review
	if err := c.doJSON(req, &review); err != nil {
		return Review{}, stacktrace.Wrap(err)
	}
	return review, nil
}

func (c *GitHubClient) newRequest(ctx context.Context, method string, path string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, stacktrace.Wrap(err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.log.Debug("constructed GitHub API request",
		zap.String("method", method),
		zap.String("path", path),
	)
	return req, nil
}

func (c *GitHubClient) doJSON(req *http.Request, target any) error {
	c.log.Debug("sending GitHub API request",
		zap.String("method", req.Method),
		zap.String("path", req.URL.Path),
	)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer resp.Body.Close()

	c.log.Debug("received GitHub API response",
		zap.String("method", req.Method),
		zap.String("path", req.URL.Path),
		zap.Int("statusCode", resp.StatusCode),
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return stacktrace.Errorf("GitHub API %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return stacktrace.Errorf("decode GitHub API response: %w", err)
	}
	return nil
}
