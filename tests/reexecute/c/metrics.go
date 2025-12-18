// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
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
			query: "avalanche_meterchainvm_parse_block_sum",
			kind:  gauge,
		},
		{
			name:  "block_verify",
			query: "avalanche_meterchainvm_verify_sum",
			kind:  gauge,
		},
		{
			name:  "block_accept",
			query: "avalanche_meterchainvm_accept_sum",
			kind:  gauge,
		},
	}
)

type topLevelMetric struct {
	name  string
	query string
	kind  metricKind
}

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

func getTopLevelMetrics(tc tests.TestContext, tool *benchmarkTool, registry prometheus.Gatherer, elapsed time.Duration) {
	r := require.New(tc)

	totalGas, err := getMetricValue(registry, gasMetric)
	r.NoError(err)
	r.NotZero(totalGas, "denominator metric %q has value 0", gasMetric.name)

	var (
		mgas                      float64 = 1_000_000
		ggas                      float64 = 1_000_000_000
		nanosecondsPerMillisecond float64 = 1_000_000
	)

	mgasPerSecond := (totalGas / mgas) / elapsed.Seconds()
	tool.addResult(mgasPerSecond, "mgas/s")

	totalGGas := totalGas / ggas
	msPerGGas := (float64(elapsed) / nanosecondsPerMillisecond) / totalGGas
	tool.addResult(msPerGGas, "ms/ggas")

	for _, metric := range meterVMMetrics {
		// MeterVM counters are in terms of nanoseconds
		metricValNanoseconds, err := getMetricValue(registry, metric)
		r.NoError(err)

		msPerGGas := (metricValNanoseconds / nanosecondsPerMillisecond) / totalGGas
		tool.addResult(msPerGGas, metric.name+"_ms/ggas")
	}
}
