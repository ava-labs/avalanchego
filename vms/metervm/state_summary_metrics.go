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
	isSummaryAccepted,
	syncState metric.Averager
}

func (ssM *stateSummaryMetrics) Initialize(
	namespace string,
	reg prometheus.Registerer,
) error {
	errs := wrappers.Errs{}
	ssM.lastSummary = newAverager(namespace, "last_summary", reg, &errs)
	ssM.isSummaryAccepted = newAverager(namespace, "summary_accepted", reg, &errs)
	ssM.syncState = newAverager(namespace, "sync_state", reg, &errs)
	return errs.Err
}
