// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

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

// GetMetricValue returns the value of the specified metric which has the
// required labels.
//
// If multiple metrics match the provided labels, the first metric found is
// returned.
//
// Only Counter and Gauge metrics are supported.
func GetMetricValue(metrics NodeMetrics, name string, labels prometheus.Labels) (float64, bool) {
	metricFamily, ok := metrics[name]
	if !ok {
		return 0, false
	}

	for _, metric := range metricFamily.Metric {
		if !labelsMatch(metric, labels) {
			continue
		}

		switch {
		case metric.Gauge != nil:
			return metric.Gauge.GetValue(), true
		case metric.Counter != nil:
			return metric.Counter.GetValue(), true
		}
	}
	return 0, false
}

func labelsMatch(metric *dto.Metric, labels prometheus.Labels) bool {
	var found int
	for _, label := range metric.Label {
		expectedValue, ok := labels[label.GetName()]
		if !ok {
			continue
		}
		if label.GetValue() != expectedValue {
			return false
		}
		found++
	}
	return found == len(labels)
}
