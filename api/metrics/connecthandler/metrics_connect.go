// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connecthandler

import (
	"context"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/proto/pb/metrics/v1/metricsv1connect"

	metricsv1 "github.com/ava-labs/avalanchego/proto/pb/metrics/v1"
	dto "github.com/prometheus/client_model/go"
)

var _ metricsv1connect.MetricsServiceHandler = (*ConnectMetricsService)(nil)

type ConnectMetricsService struct {
	metrics prometheus.Gatherer
}

func NewConnectMetricsService(metrics prometheus.Gatherer) *ConnectMetricsService {
	return &ConnectMetricsService{metrics: metrics}
}

func (c *ConnectMetricsService) Metrics(
	_ context.Context,
	_ *connect.Request[metricsv1.MetricsRequest],
) (*connect.Response[metricsv1.MetricsResponse], error) {
	mfs, err := c.metrics.Gather()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var metricsProtoList []*metricsv1.Metric

	for _, mf := range mfs {
		for _, m := range mf.Metric {
			labels := make(map[string]string, len(m.Label))
			for _, label := range m.Label {
				labels[label.GetName()] = label.GetValue()
			}

			var value float64
			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				if m.Counter != nil {
					value = m.Counter.GetValue()
				}
			case dto.MetricType_GAUGE:
				if m.Gauge != nil {
					value = m.Gauge.GetValue()
				}
			default:
				// Handles Counter, Gauge, and Untyped metrics.
				// Histograms and Summaries require custom serialization.
				continue
			}

			metricsProtoList = append(metricsProtoList, &metricsv1.Metric{
				Name:   mf.GetName(),
				Type:   mf.GetType().String(),
				Help:   mf.GetHelp(),
				Value:  value,
				Labels: labels,
			})
		}
	}

	return connect.NewResponse(&metricsv1.MetricsResponse{
		Metrics: metricsProtoList,
	}), nil
}
