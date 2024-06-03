// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api/metrics"

	dto "github.com/prometheus/client_model/go"
)

// "metric name" -> "metric value"
type NodeMetrics map[string]*dto.MetricFamily

// URI -> "metric name" -> "metric value"
type NodesMetrics map[string]NodeMetrics

// GetNodeMetrics retrieves the specified metrics the provided node URI.
func GetNodeMetrics(ctx context.Context, nodeURI string) (NodeMetrics, error) {
	client := metrics.NewClient(nodeURI)
	return client.GetMetrics(ctx)
}

// GetNodesMetrics retrieves the specified metrics for the provided node URIs.
func GetNodesMetrics(ctx context.Context, nodeURIs []string) (NodesMetrics, error) {
	metrics := make(NodesMetrics, len(nodeURIs))
	for _, u := range nodeURIs {
		var err error
		metrics[u], err = GetNodeMetrics(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve metrics for %s: %w", u, err)
		}
	}
	return metrics, nil
}

func GetFirstMetricValue(metrics NodeMetrics, name string) (float64, bool) {
	metricFamily, ok := metrics[name]
	if !ok || len(metricFamily.Metric) < 1 {
		return 0, false
	}

	metric := metricFamily.Metric[0]
	switch {
	case metric.Gauge != nil:
		return metric.Gauge.GetValue(), true
	case metric.Counter != nil:
		return metric.Counter.GetValue(), true
	default:
		return 0, false
	}
}
