package testutils

import (
	"testing"

	"github.com/ethereum/go-ethereum/metrics"
)

// WithMetrics enables go-ethereum metrics globally for the test.
// If the [metrics.Enabled] is already true, nothing is done.
// Otherwise, it is set to true and is reverted to false when the test finishes.
func WithMetrics(t *testing.T) {
	if metrics.Enabled {
		return
	}
	metrics.Enabled = true
	t.Cleanup(func() {
		metrics.Enabled = false
	})
}
