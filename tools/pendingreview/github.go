// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
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

type pullRequestContext struct {
	ID          string
	ViewerLogin string
	Reviews     []Review
}

func (c *GitHubClient) Viewer(ctx context.Context) (User, error) {
	var resp struct {
		Viewer User `json:"viewer"`
	}
	if err := c.graphql(ctx, "query Viewer { viewer { login } }", nil, &resp); err != nil {
		return User{}, err
	}
	return resp.Viewer, nil
}

func (c *GitHubClient) CreatePendingReview(ctx context.Context, repo string, prNumber int, body string) (Review, error) {
	pullRequestID, err := c.pullRequestID(ctx, repo, prNumber)
	if err != nil {
		return Review{}, err
	}

	var resp struct {
		AddPullRequestReview struct {
			PullRequestReview graphqlReview `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}
	if err := c.graphql(ctx, `
mutation CreatePendingReview($pullRequestID: ID!, $body: String!) {
  addPullRequestReview(input: {pullRequestId: $pullRequestID, body: $body}) {
    pullRequestReview {
      id
      databaseId
      state
      body
      url
      author { login }
    }
  }
}`, map[string]any{
		"pullRequestID": pullRequestID,
		"body":          body,
	}, &resp); err != nil {
		return Review{}, err
	}
	return resp.AddPullRequestReview.PullRequestReview.toReview(nil), nil
}

func (c *GitHubClient) GetPendingReview(ctx context.Context, repo string, prNumber int, login string) (Review, error) {
	const maxAttempts = 5

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pullRequest, err := c.pullRequestContext(ctx, repo, prNumber)
		if err != nil {
			return Review{}, err
		}
		effectiveLogin := login
		if effectiveLogin == "" {
			effectiveLogin = pullRequest.ViewerLogin
		}
		review, found := FindPendingReviewForAuthor(pullRequest.Reviews, effectiveLogin)
		if found {
			return review, nil
		}
		if attempt == maxAttempts {
			return Review{}, stacktrace.Errorf("no pending review found for %s on %s#%d", effectiveLogin, repo, prNumber)
		}

		timer := time.NewTimer(time.Duration(attempt) * 200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Review{}, stacktrace.Wrap(ctx.Err())
		case <-timer.C:
		}
	}
	return Review{}, stacktrace.Errorf("no pending review found for %s on %s#%d", login, repo, prNumber)
}

func (c *GitHubClient) DeletePendingReview(ctx context.Context, reviewID string) error {
	var resp struct {
		DeletePullRequestReview struct {
			ClientMutationID string `json:"clientMutationId"`
		} `json:"deletePullRequestReview"`
	}
	return c.graphql(ctx, `
mutation DeletePendingReview($reviewID: ID!) {
  deletePullRequestReview(input: {pullRequestReviewId: $reviewID}) {
    clientMutationId
  }
}`, map[string]any{"reviewID": reviewID}, &resp)
}

func (c *GitHubClient) UpdatePendingReviewBody(ctx context.Context, reviewID string, body string) (Review, error) {
	var resp struct {
		UpdatePullRequestReview struct {
			PullRequestReview graphqlReview `json:"pullRequestReview"`
		} `json:"updatePullRequestReview"`
	}
	if err := c.graphql(ctx, `
mutation UpdatePendingReviewBody($reviewID: ID!, $body: String!) {
  updatePullRequestReview(input: {pullRequestReviewId: $reviewID, body: $body}) {
    pullRequestReview {
      id
      databaseId
      state
      body
      url
      author { login }
    }
  }
}`, map[string]any{
		"reviewID": reviewID,
		"body":     body,
	}, &resp); err != nil {
		return Review{}, err
	}
	return resp.UpdatePullRequestReview.PullRequestReview.toReview(nil), nil
}

func (c *GitHubClient) ReplacePendingReviewEntries(ctx context.Context, reviewID string, liveEntries []ReviewComment, desiredEntries []DraftReviewEntry) error {
	liveCommentsByKey := entriesByKey(liveEntries)
	desiredByKey := draftEntriesByKey(desiredEntries)

	var deletes []ReviewComment
	for key, comments := range liveCommentsByKey {
		keep := desiredByKey[key]
		if len(comments) <= len(keep) {
			continue
		}
		deletes = append(deletes, comments[len(keep):]...)
	}
	slices.SortFunc(deletes, compareReviewCommentsForDelete)
	for _, comment := range deletes {
		if err := c.deletePendingReviewComment(ctx, comment.ID); err != nil {
			return err
		}
	}

	var creates []DraftReviewEntry
	for key, entries := range desiredByKey {
		keep := liveCommentsByKey[key]
		if len(entries) <= len(keep) {
			continue
		}
		creates = append(creates, entries[len(keep):]...)
	}
	creates = normalizeDraftReviewEntries(creates)
	for _, entry := range creates {
		if err := c.createPendingReviewEntry(ctx, reviewID, entry); err != nil {
			return err
		}
	}
	return nil
}

func (c *GitHubClient) createPendingReviewEntry(ctx context.Context, reviewID string, entry DraftReviewEntry) error {
	switch entry.Kind {
	case DraftReviewEntryKindNewThread:
		_, err := c.addPendingReviewThread(ctx, reviewID, entry)
		return err
	case DraftReviewEntryKindThreadReply:
		_, err := c.addPendingReviewThreadReply(ctx, reviewID, entry)
		return err
	default:
		return stacktrace.Errorf("unsupported draft review entry kind %q", entry.Kind)
	}
}

func (c *GitHubClient) addPendingReviewThread(ctx context.Context, reviewID string, entry DraftReviewEntry) (ReviewComment, error) {
	input := map[string]any{
		"pullRequestReviewId": reviewID,
		"path":                entry.Path,
		"line":                entry.Line,
		"side":                entry.Side,
		"body":                entry.Body,
	}
	if entry.StartLine > 0 {
		input["startLine"] = entry.StartLine
		input["startSide"] = entry.StartSide
	}

	var resp struct {
		AddPullRequestReviewThread struct {
			Thread graphqlReviewThread `json:"thread"`
		} `json:"addPullRequestReviewThread"`
	}
	if err := c.graphql(ctx, `
mutation AddPendingReviewThread($input: AddPullRequestReviewThreadInput!) {
  addPullRequestReviewThread(input: $input) {
    thread {
      id
      comments(first: 10) {
        nodes {
          id
          body
          path
          line
          startLine
          author { login }
          replyTo { id }
          pullRequestReview { id }
        }
      }
    }
  }
}`, map[string]any{"input": input}, &resp); err != nil {
		return ReviewComment{}, err
	}
	comments := commentsFromGraphQLThread(resp.AddPullRequestReviewThread.Thread, reviewID)
	if len(comments) == 0 {
		return ReviewComment{}, stacktrace.New("GitHub returned no draft comments for created review thread")
	}
	return comments[0], nil
}

func (c *GitHubClient) addPendingReviewThreadReply(ctx context.Context, reviewID string, entry DraftReviewEntry) (ReviewComment, error) {
	var resp struct {
		AddPullRequestReviewThreadReply struct {
			Comment graphqlReviewComment `json:"comment"`
		} `json:"addPullRequestReviewThreadReply"`
	}
	if err := c.graphql(ctx, `
mutation AddPendingReviewThreadReply($reviewID: ID!, $threadID: ID!, $body: String!) {
  addPullRequestReviewThreadReply(input: {
    pullRequestReviewId: $reviewID
    pullRequestReviewThreadId: $threadID
    body: $body
  }) {
    comment {
      id
      body
      path
      line
      startLine
      author { login }
      replyTo { id }
      pullRequestReview { id }
    }
  }
}`, map[string]any{
		"reviewID": reviewID,
		"threadID": entry.ThreadID,
		"body":     entry.Body,
	}, &resp); err != nil {
		return ReviewComment{}, err
	}
	comment := resp.AddPullRequestReviewThreadReply.Comment.toReviewComment(entry.ThreadID)
	comment.Kind = DraftReviewEntryKindThreadReply
	return comment, nil
}

func (c *GitHubClient) deletePendingReviewComment(ctx context.Context, commentID string) error {
	var resp struct {
		DeletePullRequestReviewComment struct {
			ClientMutationID string `json:"clientMutationId"`
		} `json:"deletePullRequestReviewComment"`
	}
	return c.graphql(ctx, `
mutation DeletePendingReviewComment($commentID: ID!) {
  deletePullRequestReviewComment(input: {id: $commentID}) {
    clientMutationId
  }
}`, map[string]any{"commentID": commentID}, &resp)
}

func (c *GitHubClient) pullRequestID(ctx context.Context, repo string, prNumber int) (string, error) {
	pullRequest, err := c.pullRequestContext(ctx, repo, prNumber)
	if err != nil {
		return "", err
	}
	return pullRequest.ID, nil
}

func (c *GitHubClient) pullRequestContext(ctx context.Context, repo string, prNumber int) (pullRequestContext, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return pullRequestContext{}, err
	}

	var resp struct {
		Viewer     User `json:"viewer"`
		Repository struct {
			PullRequest *graphqlPullRequest `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, `
query PullRequestContext($owner: String!, $name: String!, $number: Int!) {
  viewer { login }
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      id
      reviewThreads(first: 100) {
        nodes {
          id
          comments(first: 100) {
            nodes {
              id
              body
              path
              line
              startLine
              author { login }
              replyTo { id }
              pullRequestReview { id }
            }
          }
        }
      }
      reviews(first: 20, states: [PENDING]) {
        nodes {
          id
          databaseId
          state
          body
          url
          author { login }
          comments(first: 100) {
            nodes {
              id
              body
              path
              line
              startLine
              author { login }
              replyTo { id }
              pullRequestReview { id }
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
		return pullRequestContext{}, err
	}
	if resp.Repository.PullRequest == nil {
		return pullRequestContext{}, stacktrace.Errorf("pull request not found for %s#%d", repo, prNumber)
	}

	threadByCommentID := make(map[string]string)
	for _, thread := range resp.Repository.PullRequest.ReviewThreads.Nodes {
		for _, comment := range thread.Comments.Nodes {
			threadByCommentID[comment.ID] = thread.ID
		}
	}

	reviews := make([]Review, 0, len(resp.Repository.PullRequest.Reviews.Nodes))
	for _, review := range resp.Repository.PullRequest.Reviews.Nodes {
		reviews = append(reviews, review.toReview(threadByCommentID))
	}

	return pullRequestContext{
		ID:          resp.Repository.PullRequest.ID,
		ViewerLogin: resp.Viewer.Login,
		Reviews:     reviews,
	}, nil
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *GitHubClient) graphql(ctx context.Context, query string, variables map[string]any, target any) error {
	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/graphql", payload)
	if err != nil {
		return err
	}

	var resp graphqlResponse
	if err := c.doJSON(req, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return stacktrace.Errorf("GitHub GraphQL: %s", resp.Errors[0].Message)
	}
	if target == nil || len(resp.Data) == 0 || string(resp.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(resp.Data, target); err != nil {
		return stacktrace.Errorf("decode GitHub GraphQL response: %w", err)
	}
	return nil
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

type graphqlPullRequest struct {
	ID            string `json:"id"`
	ReviewThreads struct {
		Nodes []graphqlReviewThread `json:"nodes"`
	} `json:"reviewThreads"`
	Reviews struct {
		Nodes []graphqlReview `json:"nodes"`
	} `json:"reviews"`
}

type graphqlReview struct {
	ID         string `json:"id"`
	DatabaseID int64  `json:"databaseId"`
	State      string `json:"state"`
	Body       string `json:"body"`
	URL        string `json:"url"`
	Author     User   `json:"author"`
	Comments   struct {
		Nodes []graphqlReviewComment `json:"nodes"`
	} `json:"comments"`
}

type graphqlReviewThread struct {
	ID       string `json:"id"`
	Comments struct {
		Nodes []graphqlReviewComment `json:"nodes"`
	} `json:"comments"`
}

type graphqlReviewComment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	StartLine int    `json:"startLine"`
	Author    User   `json:"author"`
	ReplyTo   *struct {
		ID string `json:"id"`
	} `json:"replyTo"`
	PullRequestReview *struct {
		ID string `json:"id"`
	} `json:"pullRequestReview"`
}

func (r graphqlReview) toReview(threadByCommentID map[string]string) Review {
	comments := make([]ReviewComment, 0, len(r.Comments.Nodes))
	for _, comment := range r.Comments.Nodes {
		threadID := ""
		if threadByCommentID != nil {
			threadID = threadByCommentID[comment.ID]
		}
		reviewComment := comment.toReviewComment(threadID)
		if reviewComment.Kind == "" {
			reviewComment.Kind = DraftReviewEntryKindNewThread
		}
		comments = append(comments, reviewComment)
	}
	return Review{
		ID:         r.ID,
		DatabaseID: r.DatabaseID,
		State:      r.State,
		Body:       r.Body,
		HTMLURL:    r.URL,
		User:       r.Author,
		Comments:   normalizeReviewComments(comments),
	}
}

func (c graphqlReviewComment) toReviewComment(threadID string) ReviewComment {
	kind := DraftReviewEntryKindNewThread
	replyToCommentID := ""
	if c.ReplyTo != nil {
		kind = DraftReviewEntryKindThreadReply
		replyToCommentID = c.ReplyTo.ID
	}
	return ReviewComment{
		ID:               c.ID,
		ThreadID:         threadID,
		Kind:             kind,
		ReplyToCommentID: replyToCommentID,
		Path:             c.Path,
		Line:             c.Line,
		Side:             "",
		StartLine:        c.StartLine,
		StartSide:        "",
		Body:             c.Body,
		User:             c.Author,
	}
}

func commentsFromGraphQLThread(thread graphqlReviewThread, reviewID string) []ReviewComment {
	comments := make([]ReviewComment, 0, len(thread.Comments.Nodes))
	for _, comment := range thread.Comments.Nodes {
		if comment.PullRequestReview == nil || comment.PullRequestReview.ID != reviewID {
			continue
		}
		comments = append(comments, comment.toReviewComment(thread.ID))
	}
	return normalizeReviewComments(comments)
}
