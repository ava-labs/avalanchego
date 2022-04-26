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
	getOngoingSyncStateSummary,
	getLastStateSummary,
	parseStateSummary,
	getStateSummary metric.Averager
}

func newStateSummaryMetrics(namespace string, reg prometheus.Registerer) (stateSummaryMetrics, error) {
	errs := wrappers.Errs{}
	return stateSummaryMetrics{
		stateSyncEnabled:           newAverager(namespace, "state_sync_enabled", reg, &errs),
		getOngoingSyncStateSummary: newAverager(namespace, "get_ongoing_state_sync_summary", reg, &errs),
		getLastStateSummary:        newAverager(namespace, "get_last_state_summary", reg, &errs),
		parseStateSummary:          newAverager(namespace, "parse_state_summary", reg, &errs),
		getStateSummary:            newAverager(namespace, "get_state_summary", reg, &errs),
	}, errs.Err
}
