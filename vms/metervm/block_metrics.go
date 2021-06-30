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
	buildBlockErr,
	parseBlock,
	parseBlockErr,
	getBlock,
	getBlockErr,
	setPreference,
	lastAccepted,
	verify,
	verifyErr,
	accept,
	reject prometheus.Histogram
}

func (m *blockMetrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.buildBlock = metric.NewNanosecondsLatencyMetric(namespace, "build_block")
	m.buildBlockErr = metric.NewNanosecondsLatencyMetric(namespace, "build_block_err")
	m.parseBlock = metric.NewNanosecondsLatencyMetric(namespace, "parse_block")
	m.parseBlockErr = metric.NewNanosecondsLatencyMetric(namespace, "parse_block_err")
	m.getBlock = metric.NewNanosecondsLatencyMetric(namespace, "get_block")
	m.getBlockErr = metric.NewNanosecondsLatencyMetric(namespace, "get_block_err")
	m.setPreference = metric.NewNanosecondsLatencyMetric(namespace, "set_preference")
	m.lastAccepted = metric.NewNanosecondsLatencyMetric(namespace, "last_accepted")
	m.verify = metric.NewNanosecondsLatencyMetric(namespace, "verify")
	m.verifyErr = metric.NewNanosecondsLatencyMetric(namespace, "verify_err")
	m.accept = metric.NewNanosecondsLatencyMetric(namespace, "accept")
	m.reject = metric.NewNanosecondsLatencyMetric(namespace, "reject")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.buildBlock),
		registerer.Register(m.buildBlockErr),
		registerer.Register(m.parseBlock),
		registerer.Register(m.parseBlockErr),
		registerer.Register(m.getBlock),
		registerer.Register(m.getBlockErr),
		registerer.Register(m.setPreference),
		registerer.Register(m.lastAccepted),
		registerer.Register(m.verify),
		registerer.Register(m.verifyErr),
		registerer.Register(m.accept),
		registerer.Register(m.reject),
	)
	return errs.Err
}
