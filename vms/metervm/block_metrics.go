// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type blockMetrics struct {
	buildBlock,
	parseBlock,
	getBlock,
	setPreference,
	lastAccepted prometheus.Histogram
}

func (m *blockMetrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.buildBlock = metric.NewNanosecondsLatencyMetric(namespace, "build_block")
	m.parseBlock = metric.NewNanosecondsLatencyMetric(namespace, "parse_block")
	m.getBlock = metric.NewNanosecondsLatencyMetric(namespace, "get_block")
	m.setPreference = metric.NewNanosecondsLatencyMetric(namespace, "set_preference")
	m.lastAccepted = metric.NewNanosecondsLatencyMetric(namespace, "last_accepted")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.buildBlock),
		registerer.Register(m.parseBlock),
		registerer.Register(m.getBlock),
		registerer.Register(m.setPreference),
		registerer.Register(m.lastAccepted),
	)
	return errs.Err
}
