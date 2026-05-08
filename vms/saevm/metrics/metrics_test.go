// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMetrics(t *testing.T) {
	metrics, err := New(prometheus.NewRegistry())
	require.NoError(t, err, "New()")

	metrics.MarkBlockExecuted(6)
	metrics.MarkBlockSettled(7)

	require.Equal(t, float64(6), testutil.ToFloat64(metrics.LastExecutedHeight), "last executed height")
	require.Equal(t, float64(7), testutil.ToFloat64(metrics.LastSettledHeight), "last settled height")
}
