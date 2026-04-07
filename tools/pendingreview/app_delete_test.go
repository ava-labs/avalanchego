// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestRunDeleteEnsureAbsentDeletesReviewAndVerifiesAbsence(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru",
		ReviewID:          "review-123",
		LastPublishedBody: "stored body",
	}))

	var getCount int
	server := newGraphQLTestServer(t, func(t *testing.T, query string, variables map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestPendingReview"):
			getCount++
			if getCount == 1 {
				return graphQLPullRequestMetadataData("live body")
			}
			return graphQLNoPendingReviewMetadataData()
		case strings.Contains(query, "mutation DeletePendingReview"):
			require.Equal(t, "review-123", variables["reviewID"])
			return map[string]any{
				"deletePullRequestReview": map[string]any{"clientMutationId": "deleted"},
			}
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"delete",
		"--pr", "5168",
		"--ensure-absent",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	_, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.EqualError(t, err, "no stored review state for ava-labs/avalanchego#5168 as maru; run create first or use --force")
	require.Contains(t, stdout.String(), "Deleted pending review 123")
	require.Contains(t, stdout.String(), "Verified no pending review remains")
	require.GreaterOrEqual(t, getCount, 2)
}

func TestRunDeleteEnsureAbsentSucceedsWhenAlreadyAbsent(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store := NewStateStore(logging.NoLog{}, stateDir)
	require.NoError(t, store.Save(ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru",
		ReviewID:          "review-123",
		LastPublishedBody: "stored body",
	}))

	server := newGraphQLTestServer(t, func(t *testing.T, query string, _ map[string]any) any {
		switch {
		case strings.Contains(query, "query Viewer"):
			return map[string]any{"viewer": map[string]any{"login": "maru"}}
		case strings.Contains(query, "query PullRequestPendingReview"):
			return graphQLNoPendingReviewMetadataData()
		default:
			require.FailNowf(t, "unexpected query", "query: %s", query)
			return nil
		}
	})
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, io.Discard)
	app.tokenProvider = staticTokenProvider{token: "test-token"}
	app.httpClient = server.Client()
	app.baseURL = server.URL

	require.NoError(t, app.Run(t.Context(), []string{
		"delete",
		"--pr", "5168",
		"--ensure-absent",
		"--state-dir", stateDir,
		"--config-dir", t.TempDir(),
	}))

	_, err := store.Load("ava-labs/avalanchego", "maru", 5168)
	require.EqualError(t, err, "no stored review state for ava-labs/avalanchego#5168 as maru; run create first or use --force")
	require.Contains(t, stdout.String(), "No pending review found")
}
