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
		Name:    "gas",
		Queries: []string{"avalanche_evm_eth_chain_block_gas_used_processed"},
	}
	topLevelMetrics = []topLevelMetric{
		{
			Name:    "content_validation",
			Queries: []string{"avalanche_evm_eth_chain_block_validations_content"},
		},
		{
			Name: "state_init",
			Queries: []string{
				"avalanche_evm_eth_chain_block_inits_state",
			},
		},
		{
			Name: "execution",
			Queries: []string{
				"avalanche_evm_eth_chain_block_executions",
			},
		},
		{
			Name: "block_validation",
			Queries: []string{
				"avalanche_evm_eth_chain_block_validations_state",
			},
		},
		{
			Name: "trie_hash",
			Queries: []string{
				"avalanche_evm_eth_chain_storage_hashes",
				"avalanche_evm_eth_chain_account_hashes",
			},
		},
		{
			Name: "trie_update",
			Queries: []string{
				"avalanche_evm_eth_chain_account_updates",
				"avalanche_evm_eth_chain_storage_updates",
			},
		},
		{
			Name: "trie_read",
			Queries: []string{
				"avalanche_evm_eth_chain_snapshot_account_reads",
				"avalanche_evm_eth_chain_account_reads",
				"avalanche_evm_eth_chain_snapshot_storage_reads",
				"avalanche_evm_eth_chain_storage_reads",
			},
		},
		{
			Name: "block_write",
			Queries: []string{
				"avalanche_evm_eth_chain_block_writes",
			},
		},
		{
			Name: "commit",
			Queries: []string{
				"avalanche_evm_eth_chain_account_commits",
				"avalanche_evm_eth_chain_storage_commits",
				"avalanche_evm_eth_chain_snapshot_commits",
				"avalanche_evm_eth_chain_triedb_commits",
			},
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

type topLevelMetric struct {
	Name    string
	Queries []string
}

func calcMetric(tb testing.TB, m topLevelMetric, registry prometheus.Gatherer) float64 {
	r := require.New(tb)
	sum := float64(0)
	for _, query := range m.Queries {
		val, err := getCounterMetricValue(registry, query)
		r.NoError(err, "failed to get counter value for metric %q query %q", m.Name, query)
		sum += val
	}
	return sum
}

func getTopLevelMetrics(b *testing.B, registry prometheus.Gatherer, elapsed time.Duration) {
	r := require.New(b)

	totalGas := calcMetric(b, gasMetric, registry)
	r.NotZero(totalGas, "denominator metric %q has value 0", gasMetric.Name)

	mgasPerSecond := totalGas / 1_000_000 / elapsed.Seconds() // mega
	b.ReportMetric(mgasPerSecond, fmt.Sprintf("m%s/s", gasMetric.Name))

	totalGGas := totalGas / 1_000_000_000 // giga

	totalMSTrackedPerGGas := float64(0)
	for _, metric := range topLevelMetrics {
		metricValMS := calcMetric(b, metric, registry) / (totalGGas) // metric / ggas
		totalMSTrackedPerGGas += metricValMS
		b.ReportMetric(metricValMS, fmt.Sprintf("%s_ms/g%s", metric.Name, gasMetric.Name))
	}
	totalSTracked := totalMSTrackedPerGGas / 1000
	b.ReportMetric(totalSTracked, "s_tracked")
	b.ReportMetric(elapsed.Seconds(), "s_total")
	b.ReportMetric(totalSTracked/elapsed.Seconds(), "s_tracked/s_total")
}
