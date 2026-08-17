// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	dto "github.com/prometheus/client_model/go"
)

var (
	expectedMetrics = map[string]dto.MetricType{
		"firewood_proposal_commits_total":         dto.MetricType_COUNTER,
		"firewood_flush_duration_seconds":         dto.MetricType_HISTOGRAM,
		"firewood_persist_cycle_duration_seconds": dto.MetricType_HISTOGRAM,
		"firewood_node_inserts_total":             dto.MetricType_COUNTER,
		"firewood_storage_bytes_appended_total":   dto.MetricType_COUNTER,
		// jemalloc memory allocator gauges (bytes).
		// jemalloc_retained_bytes is omitted because it can legitimately be zero
		// on some platforms, and we assert that gauge values are positive below.
		"jemalloc_active_bytes":    dto.MetricType_GAUGE,
		"jemalloc_allocated_bytes": dto.MetricType_GAUGE,
		"jemalloc_metadata_bytes":  dto.MetricType_GAUGE,
		"jemalloc_mapped_bytes":    dto.MetricType_GAUGE,
		"jemalloc_resident_bytes":  dto.MetricType_GAUGE,
	}
	initMetrics   sync.Once
	initLogs      sync.Once
	activeLogPath string
)

func ensureMetricsStarted(t *testing.T) {
	t.Helper()
	initMetrics.Do(func() {
		require.NoError(t, StartMetrics())
	})
}

func ensureLogsStarted(t *testing.T, logPath string) {
	t.Helper()
	initLogs.Do(func() {
		logConfig := &LogConfig{
			Path:        logPath,
			FilterLevel: "trace",
		}
		if err := StartLogs(logConfig); err != nil {
			// "Logging is not available" error occurs when the FFI library was
			// built without the "logger" feature flag enabled.
			require.ErrorContains(t, err, "Logging is not available")
			activeLogPath = ""
			return
		}
		activeLogPath = logPath
	})
}

func newDbWithMetricsAndLogs(t *testing.T, opts ...Option) (db *Database, logPath string) {
	t.Helper()
	db = newTestDatabase(t, opts...)
	ensureMetricsStarted(t)
	ensureLogsStarted(t, filepath.Join(t.TempDir(), "firewood.log"))
	return db, activeLogPath
}

// TestMetrics verifies that expected metrics are populated after a database
// operation. This lives under one test as we can only instantiate the global
// recorder once.
func TestMetrics(t *testing.T) {
	r := require.New(t)

	db, logPath := newDbWithMetricsAndLogs(t)
	// batch update
	_, _, batch := kvForTest(10)
	_, err := db.Update(batch)
	r.NoError(err)

	// Close database to ensure background persistence completes before checking metrics.
	// The flush_nodes metric is recorded during persistence, which happens asynchronously.
	r.NoError(db.Close(t.Context()))

	families, err := GatherRenderedMetrics()
	r.NoError(err)
	r.NotEmpty(families)

	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, mf := range families {
		byName[mf.GetName()] = mf
	}

	for name, wantType := range expectedMetrics {
		mf, ok := byName[name]
		r.True(ok, "metric %q not found", name)
		r.Equal(wantType, mf.GetType(), "metric %q has wrong type", name)

		// Jemalloc gauges must report positive byte counts in a running process.
		if wantType == dto.MetricType_GAUGE && len(mf.Metric) > 0 && mf.Metric[0].Gauge != nil {
			r.Greater(*mf.Metric[0].Gauge.Value, 0.0, "metric %q should be positive", name)
		}
	}

	if logPath != "" {
		r.True(assertNonEmptyFile(t, logPath))
	}
}

func TestGatherRenderedMetrics(t *testing.T) {
	r := require.New(t)

	// Ensure the metrics recorder is initialized.
	ensureMetricsStarted(t)

	// Call gather multiple times so the histogram accumulates observations.
	const gatherCalls = 3
	var allFamilies []*dto.MetricFamily
	for range gatherCalls {
		families, err := GatherRenderedMetrics()
		r.NoError(err)
		r.NotEmpty(families)
		allFamilies = families
	}

	// Find the native histogram metric for gather duration.
	var histFamily *dto.MetricFamily
	for _, mf := range allFamilies {
		// prometheus metric names are normalized to lowercase with underscores
		if mf.GetName() == "firewood_gather_duration_seconds" {
			histFamily = mf
			break
		}
	}
	r.NotNil(histFamily, "firewood_gather_duration_seconds metric not found")
	r.Equal(dto.MetricType_HISTOGRAM, histFamily.GetType())
	r.NotEmpty(histFamily.GetMetric())

	hist := histFamily.GetMetric()[0].GetHistogram()
	r.NotNil(hist, "histogram field must be set")

	// We called gather at least gatherCalls times; each call records one observation.
	// The first call won't see itself, but subsequent calls see prior observations.
	r.GreaterOrEqual(hist.GetSampleCount(), uint64(gatherCalls-1),
		"expected at least %d observations", gatherCalls-1)
	r.Greater(hist.GetSampleSum(), 0.0, "sample sum should be positive")

	// Validate native histogram fields are populated.
	r.NotNil(hist.Schema, "native histogram schema must be set")
	r.NotNil(hist.ZeroThreshold, "native histogram zero_threshold must be set")
	r.NotEmpty(hist.GetPositiveSpan(), "native histogram should have positive spans")
	r.NotEmpty(hist.GetPositiveDelta(), "native histogram should have positive deltas")
}

func assertNonEmptyFile(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, f)
	return true
}
