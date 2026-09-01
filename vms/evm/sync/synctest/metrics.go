// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	dto "github.com/prometheus/client_model/go"
)

// NewRequestMetrics returns request [network.Metrics] on a registry private to
// the test.
func NewRequestMetrics(tb testing.TB) *network.Metrics {
	tb.Helper()
	m, err := network.NewMetrics(prometheus.NewRegistry(), "test")
	require.NoError(tb, err, "network.NewMetrics()")
	return m
}

// HistogramSampleCount returns the number of observations recorded on h.
func HistogramSampleCount(tb testing.TB, h prometheus.Histogram) uint64 {
	tb.Helper()
	var pb dto.Metric
	require.NoError(tb, h.Write(&pb), "histogram.Write()")
	return pb.GetHistogram().GetSampleCount()
}
