// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type tempoSearchResponse struct {
	Traces []struct {
		TraceID string `json:"traceID"`
	} `json:"traces"`
}

// CheckTracesExist checks that at least one trace spanning two or more nodes
// exists for the given network. If no network UUID is provided, an attempt will
// be made to derive selectors from env vars (GH_*) identifying a github actions
// run.
func CheckTracesExist(ctx context.Context, log logging.Logger, networkUUID string) error {
	config, err := getCollectorConfigForQuery(alloyCmd)
	if err != nil {
		return stacktrace.Errorf("failed to get collector credentials: %w", err)
	}

	selectors, err := getTraceQLSelectors(networkUUID)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	traceql := fmt.Sprintf("{ %s && span.remote_parent = true }", selectors)

	log.Info("checking if cross-node traces exist",
		zap.String("url", config.url),
		zap.String("query", traceql),
	)

	err = waitForCount(
		ctx,
		log,
		"cross-node traces",
		func() (int, error) {
			return crossNodeTraceCount(ctx, log, config, traceql)
		},
	)
	if err != nil {
		return stacktrace.Errorf("failed to find cross-node traces: %w", err)
	}
	return nil
}

// crossNodeTraceCount returns the number of traces matching traceql that
// contain spans from at least two distinct nodes.
func crossNodeTraceCount(
	ctx context.Context,
	log logging.Logger,
	config collectorConfig,
	traceql string,
) (int, error) {
	now := time.Now()
	params := url.Values{}
	params.Add("q", traceql)
	params.Add("start", strconv.FormatInt(now.Add(-time.Hour).Unix(), 10))
	params.Add("end", strconv.FormatInt(now.Unix(), 10))
	params.Add("limit", "20")

	body, err := queryTempo(ctx, config, fmt.Sprintf("%s/api/search?%s", config.url, params.Encode()))
	if err != nil {
		return 0, stacktrace.Wrap(err)
	}
	var search tempoSearchResponse
	if err := json.Unmarshal(body, &search); err != nil {
		return 0, stacktrace.Errorf("failed to parse search response: %w", err)
	}

	count := 0
	nodeCounts := make(map[string]int, len(search.Traces))
	for _, trace := range search.Traces {
		body, err := queryTempo(ctx, config, fmt.Sprintf("%s/api/traces/%s", config.url, url.PathEscape(trace.TraceID)))
		if err != nil {
			return 0, stacktrace.Wrap(err)
		}
		ids, err := distinctNodeIDs(body)
		if err != nil {
			return 0, stacktrace.Wrap(err)
		}
		nodeCounts[trace.TraceID] = len(ids)
		if len(ids) >= 2 {
			count++
		}
	}

	if count == 0 {
		for traceID, nodes := range nodeCounts {
			log.Info("trace does not span multiple nodes",
				zap.String("traceID", traceID),
				zap.Int("nodes", nodes),
			)
		}
	}
	return count, nil
}

// queryTempo performs an authenticated GET and returns the response body.
func queryTempo(ctx context.Context, config collectorConfig, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, stacktrace.Errorf("failed to create request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(config.username + ":" + config.password))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")

	//nolint:bodyclose // body is closed via rpc.CleanlyCloseBody
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, stacktrace.Errorf("failed to execute request: %w", err)
	}
	defer rpc.CleanlyCloseBody(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, stacktrace.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, stacktrace.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// distinctNodeIDs returns the set of node_id resource attribute values in a
// Tempo v1 trace response.
func distinctNodeIDs(traceJSON []byte) (map[string]struct{}, error) {
	var trace struct {
		Batches []struct {
			Resource struct {
				Attributes []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"resource"`
		} `json:"batches"`
	}
	if err := json.Unmarshal(traceJSON, &trace); err != nil {
		return nil, stacktrace.Errorf("failed to parse trace: %w", err)
	}

	ids := map[string]struct{}{}
	for _, batch := range trace.Batches {
		for _, attr := range batch.Resource.Attributes {
			if attr.Key == "node_id" && attr.Value.StringValue != "" {
				ids[attr.Value.StringValue] = struct{}{}
			}
		}
	}
	return ids, nil
}
