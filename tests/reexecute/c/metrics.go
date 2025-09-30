// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

var (
	gasMetric = topLevelMetric{
		name:  "gas",
		query: "avalanche_evm_eth_chain_block_gas_used_processed",
	}
	meterVMMetrics = []topLevelMetric{
		{
			name:  "block_parse",
			query: "avalanche_meterchainvm_C_parse_block_sum",
		},
		{
			name:  "block_verify",
			query: "avalanche_meterchainvm_C_verify_sum",
		},
		{
			name:  "block_accept",
			query: "avalanche_meterchainvm_C_accept_sum",
		},
	}
)

func getCounterMetricValue(registry prometheus.Gatherer, query string) (float64, error) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		return 0, fmt.Errorf("failed to gather metrics: %w", err)
	}

	for _, mf := range metricFamilies {
		if mf.GetName() == query {
			return mf.GetMetric()[0].Counter.GetValue(), nil
		}
	}

	return 0, fmt.Errorf("metric %s not found", query)
}

func getGaugeMetricValue(registry prometheus.Gatherer, query string) (float64, error) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		return 0, fmt.Errorf("failed to gather metrics: %w", err)
	}

	for _, mf := range metricFamilies {
		if mf.GetName() == query {
			return mf.GetMetric()[0].Gauge.GetValue(), nil
		}
	}

	return 0, fmt.Errorf("metric %s not found", query)
}

type topLevelMetric struct {
	name  string
	query string
}

func getTopLevelMetrics(b *testing.B, registry prometheus.Gatherer, elapsed time.Duration) {
	r := require.New(b)

	totalGas, err := getCounterMetricValue(registry, gasMetric.query)
	r.NoError(err)
	r.NotZero(totalGas, "denominator metric %q has value 0", gasMetric.name)

	var (
		gGas    float64 = 1_000_000_000
		nsPerMs float64 = 1_000_000
	)

	totalGGas := totalGas / gGas
	msPerGGas := (float64(elapsed) / nsPerMs) / totalGGas
	b.ReportMetric(msPerGGas, "ms/gGas")

	totalMSTrackedPerGGas := float64(0)
	for _, metric := range meterVMMetrics {
		metricVal, err := getGaugeMetricValue(registry, metric.query)
		r.NoError(err)

		metricValMS := (metricVal / nsPerMs) / totalGGas
		totalMSTrackedPerGGas += metricValMS
		b.ReportMetric(metricValMS, metric.name+"_ms/gGas")
	}

	b.ReportMetric(totalMSTrackedPerGGas, "tracked_ms/gGas")
}
