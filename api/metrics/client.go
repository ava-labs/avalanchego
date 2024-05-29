// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/prometheus/common/expfmt"

	dto "github.com/prometheus/client_model/go"
)

// Client for requesting metrics from a remote AvalancheGo instance
type Client struct {
	uri string
}

// NewClient returns a new Metrics API Client
func NewClient(uri string) *Client {
	return &Client{
		uri: uri + "/ext/metrics",
	}
}

// GetMetrics returns the metrics from the connected node. The metrics are
// returned as a map of metric family name to the metric family.
func (c *Client) GetMetrics(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	uri, err := url.Parse(c.uri)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		uri.String(),
		bytes.NewReader(nil),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to issue request: %w", err)
	}

	// Return an error for any non successful status code
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return nil, fmt.Errorf("received status code: %d", resp.StatusCode)
	}

	var parser expfmt.TextParser
	metrics, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return nil, err
	}
	return metrics, resp.Body.Close()
}
