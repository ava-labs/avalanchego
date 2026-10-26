// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestGetTraceQLSelectors(t *testing.T) {
	got, err := getTraceQLSelectors("abc")
	require.NoError(t, err, "getTraceQLSelectors(uuid)")
	require.Equal(t, `resource.network_uuid = "abc"`, got, "uuid selector")

	for _, l := range githubLabels {
		t.Setenv(strings.ToUpper(l), "")
	}
	t.Setenv("GH_RUN_ID", "7")
	t.Setenv("GH_JOB_ID", "j")
	got, err = getTraceQLSelectors("")
	require.NoError(t, err, "getTraceQLSelectors(gh)")
	require.Equal(t, `resource.gh_run_id = "7" && resource.gh_job_id = "j"`, got, "gh selectors in githubLabels order")
}

const (
	oneNodeTrace = `{"batches":[
	  {"resource":{"attributes":[{"key":"node_id","value":{"stringValue":"NodeID-A"}}]},"scopeSpans":[]}]}`
	twoNodeTrace = `{"batches":[
	  {"resource":{"attributes":[{"key":"node_id","value":{"stringValue":"NodeID-A"}}]},"scopeSpans":[]},
	  {"resource":{"attributes":[{"key":"node_id","value":{"stringValue":"NodeID-B"}}]},"scopeSpans":[]},
	  {"resource":{"attributes":[{"key":"node_id","value":{"stringValue":"NodeID-A"}}]},"scopeSpans":[]}]}`
)

func TestDistinctNodeIDs(t *testing.T) {
	ids, err := distinctNodeIDs([]byte(twoNodeTrace))
	require.NoError(t, err, "distinctNodeIDs()")
	require.Len(t, ids, 2, "distinct node ids")
}

func TestCrossNodeTraceCount(t *testing.T) {
	const traceql = `{ resource.network_uuid = "abc" && span.remote_parent = true }`
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			t.Errorf("unexpected Authorization header on %s: %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected Accept header on %s: %q", r.URL.Path, r.Header.Get("Accept"))
		}
		switch r.URL.Path {
		case "/api/search":
			if got := r.URL.Query().Get("q"); got != traceql {
				t.Errorf("unexpected q param: %q", got)
			}
			_, _ = w.Write([]byte(`{"traces":[{"traceID":"t1"},{"traceID":"t2"}]}`))
		case "/api/traces/t1":
			_, _ = w.Write([]byte(oneNodeTrace))
		case "/api/traces/t2":
			_, _ = w.Write([]byte(twoNodeTrace))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := collectorConfig{url: server.URL, username: "u", password: "p"}
	count, err := crossNodeTraceCount(t.Context(), logging.NoLog{}, cfg, traceql)
	require.NoError(t, err, "crossNodeTraceCount()")
	require.Equal(t, 1, count, "cross-node trace count")
}
