// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type messageQueueMetrics struct {
	ops               map[message.Op]prometheus.Gauge
	len               prometheus.Gauge
	nodesWithMessages prometheus.Gauge
	numExcessiveCPU   prometheus.Counter
}

func newMessageQueueMetrics(
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
	ops []message.Op,
) (*messageQueueMetrics, error) {
	namespace := fmt.Sprintf("%s_%s", metricsNamespace, "unprocessed_msgs")
	m := &messageQueueMetrics{
		len: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "len",
			Help:      "Messages ready to be processed",
		}),
		nodesWithMessages: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "nodes",
			Help:      "Nodes from which there are at least 1 message ready to be processed",
		}),
		numExcessiveCPU: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "excessive_cpu",
			Help:      "Times we deferred handling a message from a node because the node was using excessive CPU",
		}),
		ops: make(map[message.Op]prometheus.Gauge, len(ops)),
	}

	errs := wrappers.Errs{}
	errs.Add(
		metricsRegisterer.Register(m.len),
		metricsRegisterer.Register(m.nodesWithMessages),
		metricsRegisterer.Register(m.numExcessiveCPU),
	)

	for _, op := range ops {
		opStr := op.String()
		opMetric := prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_count", opStr),
			Help:      fmt.Sprintf("Number of of %s messages in the message queue.", opStr),
		})
		m.ops[op] = opMetric
		errs.Add(metricsRegisterer.Register(opMetric))
	}
	return m, errs.Err
}
