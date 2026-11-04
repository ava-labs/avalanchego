// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus_test

import (
	"testing"

	"github.com/ava-labs/libevm/metrics"
	"github.com/stretchr/testify/require"
)

// This test assumes that there are no imported packages that might change the
// default value of [metrics.Enabled]. It is therefore in package
// `prometheus_test` in case any other tests modify the variable. If any imports
// here or in the implementation do actually do so then this test may have false
// negatives.
func TestMetricsEnabledByDefault(t *testing.T) {
	require.True(t, metrics.Enabled, "libevm/metrics.Enabled")
	require.IsType(t, (*metrics.StandardCounter)(nil), metrics.NewCounter(), "metrics.NewCounter() returned wrong type")
}
