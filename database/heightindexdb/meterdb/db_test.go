// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/heightindexdb/memdb"

	dto "github.com/prometheus/client_model/go"
)

const metricsNamespace = "meterdb"

func setup(t *testing.T) (*prometheus.Registry, *Database) {
	t.Helper()

	reg := prometheus.NewRegistry()
	memDB := &memdb.Database{}

	db, err := New(reg, metricsNamespace, memDB)
	require.NoError(t, err)
	require.NotNil(t, db)

	return reg, db
}

func writeBlocks(t *testing.T, db *Database, blockCount int, blockSize int) [][]byte {
	t.Helper()

	blocks := make([][]byte, blockCount)
	for i := range blockCount {
		blockData := make([]byte, blockSize)
		prefix := fmt.Sprintf("block-%d", i)
		copy(blockData, prefix)
		blocks[i] = blockData
	}
	for i := range blockCount {
		require.NoError(t, db.Put(uint64(i), blocks[i]))
	}

	return blocks
}

func TestPutGet(t *testing.T) {
	reg, db := setup(t)

	// Create 100 fixed-size blocks (1KB each)
	const blockCount = 100
	const blockSize = 1024
	writeBlocks(t, db, blockCount, blockSize)

	// Read from blocks 0 to 119 (including non-existent ones)
	const blocksToRead = 120
	for height := range blocksToRead {
		_, err := db.Get(uint64(height))
		if errors.Is(err, database.ErrNotFound) {
			continue
		}
		require.NoError(t, err, "error getting block %d", height)
	}

	calls, duration, size := gatherMetrics(t, reg)

	// Verify put metrics
	require.InEpsilon(t, float64(blockCount), calls["put"], 0.01)
	require.InEpsilon(t, float64(blockCount*blockSize), size["put"], 0.01)
	require.Greater(t, duration["put"], float64(0))

	// Verify get metrics
	require.InEpsilon(t, float64(blocksToRead), calls["get"], 0.01)
	require.InEpsilon(t, float64(blockCount*blockSize), size["get"], 0.01)
	require.Greater(t, duration["get"], float64(0))
}

func TestHas(t *testing.T) {
	reg, db := setup(t)

	const blocksToRead = 120
	for height := range blocksToRead {
		_, err := db.Has(uint64(height))
		require.NoError(t, err)
	}

	calls, duration, size := gatherMetrics(t, reg)
	require.InEpsilon(t, float64(blocksToRead), calls["has"], 0.01)
	require.Zero(t, size["has"])
	require.Greater(t, duration["has"], float64(0))
}

func TestClose(t *testing.T) {
	reg, db := setup(t)
	require.NoError(t, db.Close())

	calls, duration, size := gatherMetrics(t, reg)
	require.InEpsilon(t, float64(1), calls["close"], 0.01)
	require.Zero(t, size["close"])
	require.Greater(t, duration["close"], float64(0))
}

func gatherMetrics(t *testing.T, reg *prometheus.Registry) (map[string]float64, map[string]float64, map[string]float64) {
	t.Helper()

	metrics, err := reg.Gather()
	require.NoError(t, err)
	require.NotEmpty(t, metrics)

	calls := extractMetricValues(metrics, "calls")
	duration := extractMetricValues(metrics, "duration")
	size := extractMetricValues(metrics, "size")
	return calls, duration, size
}

func extractMetricValues(metrics []*dto.MetricFamily, metricName string) map[string]float64 {
	result := make(map[string]float64)
	namespacedMetricName := fmt.Sprintf("%s_%s", metricsNamespace, metricName)

	for _, metric := range metrics {
		if *metric.Name == namespacedMetricName {
			for _, m := range metric.Metric {
				method := ""
				for _, label := range m.Label {
					if *label.Name == "method" {
						method = *label.Value
						break
					}
				}
				switch metricName {
				case "calls":
					result[method] = *m.Counter.Value
				case "duration":
					result[method] = *m.Gauge.Value
				case "size":
					result[method] = *m.Counter.Value
				}
			}
			break
		}
	}
	return result
}
