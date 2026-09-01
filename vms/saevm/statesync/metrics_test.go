// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"

	ethcommon "github.com/ava-labs/libevm/common"
)

// gaugeValue reads the current value of the gauge named name on reg.
func gaugeValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()

	mfs, err := reg.Gather()
	require.NoError(t, err, "reg.Gather()")
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		ms := mf.GetMetric()
		require.Lenf(t, ms, 1, "metric %q", name)
		return ms[0].GetGauge().GetValue()
	}
	t.Fatalf("gauge %q not registered", name)
	return 0
}

// requireLifecycle asserts the full set of lifecycle gauges at once, so a test
// failure shows the whole observable state rather than one field.
func requireLifecycle(t *testing.T, reg *prometheus.Registry, msg string, inProgress, summaryHeight, failed float64, started, finished bool) {
	t.Helper()

	require.Equal(t, inProgress, gaugeValue(t, reg, "in_progress"), "%s: in_progress", msg)
	require.Equal(t, summaryHeight, gaugeValue(t, reg, "summary_height"), "%s: summary_height", msg)
	require.Equal(t, failed, gaugeValue(t, reg, "failed"), "%s: failed", msg)
	require.Equal(t, started, gaugeValue(t, reg, "started_timestamp") > 0, "%s: started_timestamp set", msg)
	require.Equal(t, finished, gaugeValue(t, reg, "finished_timestamp") > 0, "%s: finished_timestamp set", msg)
}

// TestLifecycleMetrics drives [Handler.MarkSyncStarted] and
// [Handler.MarkSyncFinished] through the phases a sync passes through and
// checks the gauges an observer would poll: nothing before a sync, target and
// in-progress once started, and outcome plus timing once finished.
func TestLifecycleMetrics(t *testing.T) {
	t.Parallel()

	target := NewSummary(ethcommon.Hash(ids.GenerateTestID()), 4096)

	tests := []struct {
		name    string
		syncErr error
	}{
		{name: "success", syncErr: nil},
		{name: "failure", syncErr: errors.New("peers ran out of state")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newSUT(t).Handler
			requireLifecycle(t, h.reg, "before any sync", 0, 0, 0, false, false)

			h.MarkSyncStarted(target)
			requireLifecycle(t, h.reg, "after MarkSyncStarted", 1, float64(target.AcceptedHeight), 0, true, false)

			h.MarkSyncFinished(tt.syncErr)
			var wantFailed float64
			if tt.syncErr != nil {
				wantFailed = 1
			}
			requireLifecycle(t, h.reg, "after MarkSyncFinished", 0, float64(target.AcceptedHeight), wantFailed, true, true)

			started := gaugeValue(t, h.reg, "started_timestamp")
			finished := gaugeValue(t, h.reg, "finished_timestamp")
			require.GreaterOrEqual(t, finished, started, "finished_timestamp before started_timestamp")
			require.InDeltaf(t, float64(time.Now().Unix()), finished, 60, "finished_timestamp not near now")
		})
	}
}
