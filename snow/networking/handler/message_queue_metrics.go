// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/metric"
)

const opLabel = "op"

var opLabels = []string{opLabel}

type messageQueueMetrics struct {
	count             *prometheus.GaugeVec
	nodesWithMessages prometheus.Gauge
	numExcessiveCPU   prometheus.Counter

	// countByOp holds the children of count, resolved once per op so that
	// pushing and popping a message avoids allocating and hashing a label
	// set.
	countByOp [message.NumOps]prometheus.Gauge
}

// countOf returns the gauge tracking queued messages with op.
func (m *messageQueueMetrics) countOf(op message.Op) prometheus.Gauge {
	if op >= message.NumOps {
		// Only defined ops are expected here, but an undefined op MUST NOT
		// panic the node, so resolve its labels the slow way.
		return m.count.WithLabelValues(op.String())
	}
	return m.countByOp[op]
}

func (m *messageQueueMetrics) initialize(
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	namespace := metric.AppendNamespace(metricsNamespace, "unprocessed_msgs")
	m.count = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "messages in the queue",
		},
		opLabels,
	)
	m.nodesWithMessages = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "nodes",
		Help:      "nodes with at least 1 message ready to be processed",
	})
	m.numExcessiveCPU = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "excessive_cpu",
		Help:      "times a message has been deferred due to excessive CPU usage",
	})
	for op := range message.NumOps {
		m.countByOp[op] = m.count.WithLabelValues(op.String())
	}

	return errors.Join(
		metricsRegisterer.Register(m.count),
		metricsRegisterer.Register(m.nodesWithMessages),
		metricsRegisterer.Register(m.numExcessiveCPU),
	)
}
