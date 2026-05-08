// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks/blockstest"
)

func TestMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics, err := New(reg)
	require.NoError(t, err, "New()")

	metrics.MarkBlockExecuted(newTestBlock(t, 6))
	metrics.MarkBlockSettled(newTestBlock(t, 7))

	require.Equal(t, float64(6), testutil.ToFloat64(metrics.LastExecutedHeight), "last executed height")
	require.Equal(t, float64(7), testutil.ToFloat64(metrics.LastSettledHeight), "last settled height")
}

func newTestBlock(tb testing.TB, height int64) *blocks.Block {
	tb.Helper()

	return blockstest.NewBlock(
		tb,
		types.NewBlockWithHeader(&types.Header{
			Number: big.NewInt(height),
		}),
		nil,
		nil,
	)
}
