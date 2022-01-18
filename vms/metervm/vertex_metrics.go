// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type vertexMetrics struct {
	pending,
	parse,
	parseErr,
	get,
	getErr,
	verify,
	verifyErr,
	accept,
	reject metric.Averager
}

func (m *vertexMetrics) Initialize(
	namespace string,
	reg prometheus.Registerer,
) error {
	errs := wrappers.Errs{}
	m.pending = newAverager(namespace, "pending_txs", reg, &errs)
	m.parse = newAverager(namespace, "parse_tx", reg, &errs)
	m.parseErr = newAverager(namespace, "parse_tx_err", reg, &errs)
	m.get = newAverager(namespace, "get_tx", reg, &errs)
	m.getErr = newAverager(namespace, "get_tx_err", reg, &errs)
	m.verify = newAverager(namespace, "verify_tx", reg, &errs)
	m.verifyErr = newAverager(namespace, "verify_tx_err", reg, &errs)
	m.accept = newAverager(namespace, "accept", reg, &errs)
	m.reject = newAverager(namespace, "reject", reg, &errs)
	return errs.Err
}
