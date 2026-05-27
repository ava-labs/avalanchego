// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	dto "github.com/prometheus/client_model/go"
)

func TestSettlementMetric(t *testing.T) {
	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(t, 1, opt)

	executed := sut.runConsensusLoop(t)
	require.NoErrorf(t, executed.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", executed)

	vmTime.advanceToSettle(ctx, t, executed)
	settledBy := sut.runConsensusLoop(t)
	require.NoErrorf(t, settledBy.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", settledBy)
	require.Equal(t, float64(executed.Height()), gaugeValue(t, sut.rawVM.snowCtx.Metrics, "sae_"+lastSettledHeightName), "last settled height")
}

// gaugeValue returns the current value of a single-series gauge from `g` by
// name, failing the test if it is missing or has more than one series.
func gaugeValue(t *testing.T, g prometheus.Gatherer, name string) float64 {
	t.Helper()
	mfs, err := g.Gather()
	require.NoError(t, err, "Gather()")
	i := slices.IndexFunc(mfs, func(mf *dto.MetricFamily) bool {
		return mf.GetName() == name
	})
	require.GreaterOrEqualf(t, i, 0, "metric %q not found", name)
	series := mfs[i].GetMetric()
	require.Lenf(t, series, 1, "metric %q series count", name)
	return series[0].GetGauge().GetValue()
}
