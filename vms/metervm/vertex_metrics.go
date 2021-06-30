// (c) 2021, Ava Labs, Inc. All rights reserved.
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
	reject prometheus.Histogram
}

func (m *vertexMetrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.pending = metric.NewNanosecondsLatencyMetric(namespace, "pending_txs")
	m.parse = metric.NewNanosecondsLatencyMetric(namespace, "parse_tx")
	m.parseErr = metric.NewNanosecondsLatencyMetric(namespace, "parse_tx_err")
	m.get = metric.NewNanosecondsLatencyMetric(namespace, "get_tx")
	m.getErr = metric.NewNanosecondsLatencyMetric(namespace, "get_tx_err")
	m.verify = metric.NewNanosecondsLatencyMetric(namespace, "verify_tx")
	m.verifyErr = metric.NewNanosecondsLatencyMetric(namespace, "verify_tx_err")
	m.accept = metric.NewNanosecondsLatencyMetric(namespace, "accept")
	m.reject = metric.NewNanosecondsLatencyMetric(namespace, "reject")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.pending),
		registerer.Register(m.parse),
		registerer.Register(m.parseErr),
		registerer.Register(m.get),
		registerer.Register(m.getErr),
		registerer.Register(m.verify),
		registerer.Register(m.verifyErr),
		registerer.Register(m.accept),
		registerer.Register(m.reject),
	)
	return errs.Err
}
