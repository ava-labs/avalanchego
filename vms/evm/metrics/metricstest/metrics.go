// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metricstest

import (
	"sync"
	"testing"

	"github.com/ava-labs/libevm/metrics"
)

var metricsLock sync.Mutex

// WithMetrics enables [metrics.Enabled] for the test and prevents any other
// tests with metrics from running concurrently.
//
// If [metrics.Enabled] is modified, its original value is restored as part of
// the testing cleanup.
func WithMetrics(t testing.TB) {
	metricsLock.Lock()
	t.Cleanup(metricsLock.Unlock)
	initialValue := metrics.Enabled
	metrics.Enabled = true

	// Restore the original value
	t.Cleanup(func() {
		metrics.Enabled = initialValue
	})
}
