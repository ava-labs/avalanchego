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

type metricKind uint

const (
	counter metricKind = iota + 1
	gauge
)

var (
	gasMetric = topLevelMetric{
		name:  "gas",
		query: "avalanche_evm_eth_chain_block_gas_used_processed",
		kind:  counter,
	}
	meterVMMetrics = []topLevelMetric{
		{
			name:  "block_parse",
			query: "avalanche_meterchainvm_C_parse_block_sum",
			kind:  gauge,
		},
		{
			name:  "block_verify",
			query: "avalanche_meterchainvm_C_verify_sum",
			kind:  gauge,
		},
		{
			name:  "block_accept",
			query: "avalanche_meterchainvm_C_accept_sum",
			kind:  gauge,
		},
	}
)

func getMetricValue(registry prometheus.Gatherer, metric topLevelMetric) (float64, error) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		return 0, fmt.Errorf("failed to gather metrics: %w", err)
	}

	query := metric.query
	for _, mf := range metricFamilies {
		switch metric.kind {
		case counter:
			if mf.GetName() == query {
				return mf.GetMetric()[0].Counter.GetValue(), nil
			}
		case gauge:
			if mf.GetName() == query {
				return mf.GetMetric()[0].Gauge.GetValue(), nil
			}
		default:
			return 0, fmt.Errorf("metric type unknown: %d", metric.kind)
		}
	}

	return 0, fmt.Errorf("metric %s not found", query)
}

type topLevelMetric struct {
	name  string
	query string
	kind  metricKind
}

func getTopLevelMetrics(b *testing.B, registry prometheus.Gatherer, elapsed time.Duration) {
	r := require.New(b)

	totalGas, err := getMetricValue(registry, gasMetric)
	r.NoError(err)
	r.NotZero(totalGas, "denominator metric %q has value 0", gasMetric.name)

	var (
		mgas    float64 = 1_000_000
		ggas    float64 = 1_000_000_000
		nsPerMs float64 = 1_000_000
	)

	mgasPerSecond := (totalGas / mgas) / elapsed.Seconds()
	b.ReportMetric(mgasPerSecond, "mgas/s")

	totalGGas := totalGas / ggas
	msPerGGas := (float64(elapsed) / nsPerMs) / totalGGas
	b.ReportMetric(msPerGGas, "ms/ggas")

	totalMSTrackedPerGGas := float64(0)
	for _, metric := range meterVMMetrics {
		metricVal, err := getMetricValue(registry, metric)
		r.NoError(err)

		metricValMS := (metricVal / nsPerMs) / totalGGas
		totalMSTrackedPerGGas += metricValMS
		b.ReportMetric(metricValMS, metric.name+"_ms/ggas")
	}

	b.ReportMetric(totalMSTrackedPerGGas, "tracked_ms/ggas")
}
