// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type messageQueueMetrics struct {
	len               prometheus.Gauge
	nodesWithMessages prometheus.Gauge
	numExcessiveCPU   prometheus.Counter
}

func (m *messageQueueMetrics) initialize(
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	namespace := fmt.Sprintf("%s_%s", metricsNamespace, "unprocessed_msgs")
	m.len = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "len",
		Help:      "Messages ready to be processed",
	})
	m.nodesWithMessages = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "nodes",
		Help:      "Nodes from which there are at least 1 message ready to be processed",
	})
	m.numExcessiveCPU = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "excessive_cpu",
		Help:      "Times we deferred handling a message from a node because the node was using excessive CPU",
	})

	errs := wrappers.Errs{}
	errs.Add(
		metricsRegisterer.Register(m.len),
		metricsRegisterer.Register(m.nodesWithMessages),
		metricsRegisterer.Register(m.numExcessiveCPU),
	)
	return errs.Err
}
