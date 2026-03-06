// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dto "github.com/prometheus/client_model/go"
)

var (
	metricsPort     = uint16(3000)
	expectedMetrics = map[string]dto.MetricType{
		"ffi_batch":          dto.MetricType_COUNTER,
		"proposal_commit":    dto.MetricType_COUNTER,
		"proposal_commit_ms": dto.MetricType_COUNTER,
		"ffi_propose_ms":     dto.MetricType_COUNTER,
		"ffi_commit_ms":      dto.MetricType_COUNTER,
		"ffi_batch_ms":       dto.MetricType_COUNTER,
		"flush_nodes":        dto.MetricType_COUNTER,
		"insert":             dto.MetricType_COUNTER,
		"space_from_end":     dto.MetricType_COUNTER,
	}
	expectedExpensiveMetrics = map[string]dto.MetricType{
		"ffi_commit_ms_bucket":  dto.MetricType_HISTOGRAM,
		"ffi_propose_ms_bucket": dto.MetricType_HISTOGRAM,
		"ffi_batch_ms_bucket":   dto.MetricType_HISTOGRAM,
	}
	initMetrics   sync.Once
	initLogs      sync.Once
	activeLogPath string
)

func ensureMetricsStarted(t *testing.T) {
	t.Helper()
	initMetrics.Do(func() {
		require.NoError(t, StartMetricsWithExporter(metricsPort))
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

// Test calling metrics exporter along with gathering metrics
// This lives under one test as we can only instantiate the global recorder once
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

	assertMetrics(t, metricsPort, expectedMetrics)
	if logPath != "" {
		r.True(assertNonEmptyFile(t, logPath))
	}
}

func TestExpensiveMetrics(t *testing.T) {
	r := require.New(t)
	db, _ := newDbWithMetricsAndLogs(t, WithExpensiveMetrics())
	// batch update
	_, _, batch := kvForTest(10)
	_, err := db.Update(batch)
	r.NoError(err)

	// Close database to ensure background persistence completes before checking metrics.
	// The flush_nodes metric is recorded during persistence, which happens asynchronously.
	r.NoError(db.Close(t.Context()))

	merged := make(map[string]dto.MetricType, len(expectedMetrics)+len(expectedExpensiveMetrics))
	maps.Copy(merged, expectedMetrics)
	maps.Copy(merged, expectedExpensiveMetrics)
	assertMetrics(t, metricsPort, merged)
}

func assertNonEmptyFile(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, f)
	return true
}

func assertMetrics(t *testing.T, metricsPort uint16, expected map[string]dto.MetricType) {
	r := require.New(t)
	ctx := t.Context()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("http://localhost:%d", metricsPort),
		nil,
	)
	r.NoError(err)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	r.NoError(err)

	body, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.NoError(resp.Body.Close())

	// Check that batch op was recorded (no prefix)
	r.NotContains(string(body), "ffi_batch 0")

	g := Gatherer{}
	metricsFamily, err := g.Gather()
	r.NoError(err)

	for k, v := range expected {
		var d *dto.MetricFamily
		for _, m := range metricsFamily {
			if *m.Name == k {
				d = m
			}
		}
		r.NotNil(d)
		r.Equal(v, *d.Type)
	}
}
