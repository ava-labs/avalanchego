// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

type stateSummaryMetrics struct {
	lastSummary,
	lastSummaryBlockID,
	setLastSummaryBlockID,
	isSummaryAccepted,
	parseSummary,
	getOngoingStateSyncSummary,
	syncState metric.Averager
}

func newStateSummaryMetrics(namespace string, reg prometheus.Registerer) (stateSummaryMetrics, error) {
	errs := wrappers.Errs{}
	return stateSummaryMetrics{
		lastSummary:                newAverager(namespace, "last_summary", reg, &errs),
		lastSummaryBlockID:         newAverager(namespace, "last_summary_block_id", reg, &errs),
		setLastSummaryBlockID:      newAverager(namespace, "set_last_summary_block_id", reg, &errs),
		parseSummary:               newAverager(namespace, "parse_summary", reg, &errs),
		getOngoingStateSyncSummary: newAverager(namespace, "get_ongoing_state_sync_summary", reg, &errs),
		isSummaryAccepted:          newAverager(namespace, "summary_accepted", reg, &errs),
		syncState:                  newAverager(namespace, "sync_state", reg, &errs),
	}, errs.Err
}
