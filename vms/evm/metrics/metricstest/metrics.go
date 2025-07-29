// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metricstest

import (
	"sync"
	"testing"

	"github.com/ava-labs/libevm/metrics"
)

var metricsLock sync.Mutex

// WithMetrics enables go-ethereum metrics globally for the test.
// If the [metrics.Enabled] is already true, nothing is done.
// Otherwise, it is set to true and is reverted to false when the test finishes.
func WithMetrics(t *testing.T) {
	metricsLock.Lock()
	t.Cleanup(func() {
		metricsLock.Unlock()
	})
	if metrics.Enabled {
		return
	}
	metrics.Enabled = true
	t.Cleanup(func() {
		metrics.Enabled = false
	})
}
