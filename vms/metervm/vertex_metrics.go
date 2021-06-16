// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	get prometheus.Histogram
}

func (m *vertexMetrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.pending = metric.NewNanosecondsLatencyMetric(namespace, "pending_txs")
	m.parse = metric.NewNanosecondsLatencyMetric(namespace, "parse_tx")
	m.get = metric.NewNanosecondsLatencyMetric(namespace, "get_tx")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.pending),
		registerer.Register(m.parse),
		registerer.Register(m.get),
	)
	return errs.Err
}
