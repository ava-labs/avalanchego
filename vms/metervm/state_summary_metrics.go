// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

type stateSummaryMetrics struct {
	stateSyncEnabled,
	GetOngoingSyncStateSummary,
	getLastStateSummary,
	parseStateSummary,
	getStateSummary,
	getStateSyncResult,
	parseStateSyncableBlock metric.Averager
}

func newStateSummaryMetrics(namespace string, reg prometheus.Registerer) (stateSummaryMetrics, error) {
	errs := wrappers.Errs{}
	return stateSummaryMetrics{
		stateSyncEnabled:           newAverager(namespace, "state_sync_enabled", reg, &errs),
		GetOngoingSyncStateSummary: newAverager(namespace, "get_ongoing_state_sync_summary", reg, &errs),
		getLastStateSummary:        newAverager(namespace, "get_last_state_summary", reg, &errs),
		parseStateSummary:          newAverager(namespace, "parse_state_summary", reg, &errs),
		getStateSummary:            newAverager(namespace, "get_state_summary", reg, &errs),
		getStateSyncResult:         newAverager(namespace, "get_state_sync_results", reg, &errs),
		parseStateSyncableBlock:    newAverager(namespace, "parse_state_syncable_block", reg, &errs),
	}, errs.Err
}
