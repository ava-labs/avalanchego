// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/message"
	"github.com/chain4travel/caminogo/utils/wrappers"
)

type messageQueueMetrics struct {
	ops               map[message.Op]prometheus.Gauge
	len               prometheus.Gauge
	nodesWithMessages prometheus.Gauge
	numExcessiveCPU   prometheus.Counter
}

func (m *messageQueueMetrics) initialize(
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
	ops []message.Op,
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
	m.ops = make(map[message.Op]prometheus.Gauge, len(ops))

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

	errs.Add(
		metricsRegisterer.Register(m.len),
		metricsRegisterer.Register(m.nodesWithMessages),
		metricsRegisterer.Register(m.numExcessiveCPU),
	)
	return errs.Err
}
