// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus_test

import (
	"testing"

	"github.com/ava-labs/libevm/metrics"
	// NOTE: This test assumes that there are no imported packages that might
	// change the default value of [metrics.Enabled]. It is therefore in package
	// `prometheus_test` in case any other tests modify the variable. If any
	// imports here or in the implementation do actually do so then this test
	// may have false negatives.
	"github.com/stretchr/testify/require"
)

func TestMetricsEnabledByDefault(t *testing.T) {
	require.True(t, metrics.Enabled, "libevm/metrics.Enabled")

	switch m := metrics.NewCounter().(type) {
	case metrics.NilCounter:
		require.Failf(t, "unexpected type", "metrics.NewCounter() got %T; want %T", m, new(metrics.StandardCounter))
	case *metrics.StandardCounter:
	default:
		require.Failf(t, "unexpected type", "metrics.NewCounter() got unknown type %T", m)
	}
}
