// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// "metric name" -> "metric value"
type NodeMetrics map[string]float64

// URI -> "metric name" -> "metric value"
type NodesMetrics map[string]NodeMetrics

// GetNodeMetrics retrieves the specified metrics the provided node URI.
func GetNodeMetrics(nodeURI string, metricNames ...string) (NodeMetrics, error) {
	uri := nodeURI + "/ext/metrics"
	return GetMetricsValue(uri, metricNames...)
}

// GetNodesMetrics retrieves the specified metrics for the provided node URIs.
func GetNodesMetrics(nodeURIs []string, metricNames ...string) (NodesMetrics, error) {
	metrics := make(NodesMetrics, len(nodeURIs))
	for _, u := range nodeURIs {
		var err error
		metrics[u], err = GetNodeMetrics(u, metricNames...)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve metrics for %s: %w", u, err)
		}
	}
	return metrics, nil
}

func GetMetricsValue(url string, metrics ...string) (map[string]float64, error) {
	lines, err := getHTTPLines(url)
	if err != nil {
		return nil, err
	}
	mm := make(map[string]float64, len(metrics))
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		found, name := false, ""
		for _, name = range metrics {
			if !strings.HasPrefix(line, name) {
				continue
			}
			found = true
			break
		}
		if !found || name == "" { // no matched metric found
			continue
		}
		ll := strings.Split(line, " ")
		if len(ll) != 2 {
			continue
		}
		fv, err := strconv.ParseFloat(ll[1], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %q (%w)", ll, err)
		}
		mm[name] = fv
	}
	return mm, nil
}

func getHTTPLines(url string) ([]string, error) {
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	rd := bufio.NewReader(resp.Body)
	lines := []string{}
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			_ = resp.Body.Close()
			return nil, err
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines, resp.Body.Close()
}
