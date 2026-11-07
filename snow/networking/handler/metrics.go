// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
)

type metrics struct {
	expired             *prometheus.CounterVec // op
	messages            *prometheus.CounterVec // op
	lockingTime         prometheus.Gauge
	messageHandlingTime *prometheus.GaugeVec // op

	// Children of the op-labeled vectors above, resolved once per op so that
	// the per-message paths avoid allocating and hashing a label set.
	expiredByOp             [message.NumOps]prometheus.Counter
	messagesByOp            [message.NumOps]prometheus.Counter
	messageHandlingTimeByOp [message.NumOps]prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		expired: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "expired",
				Help: "messages dropped because the deadline expired",
			},
			opLabels,
		),
		messages: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "messages",
				Help: "messages handled",
			},
			opLabels,
		),
		messageHandlingTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "message_handling_time",
				Help: "time spent handling messages",
			},
			opLabels,
		),
		lockingTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "locking_time",
			Help: "time spent acquiring the context lock",
		}),
	}
	for op := range message.NumOps {
		opStr := op.String()
		m.expiredByOp[op] = m.expired.WithLabelValues(opStr)
		m.messagesByOp[op] = m.messages.WithLabelValues(opStr)
		m.messageHandlingTimeByOp[op] = m.messageHandlingTime.WithLabelValues(opStr)
	}
	return m, errors.Join(
		reg.Register(m.expired),
		reg.Register(m.messages),
		reg.Register(m.messageHandlingTime),
		reg.Register(m.lockingTime),
	)
}

// observeHandled records that a message with op was handled in handlingTime.
func (m *metrics) observeHandled(op message.Op, handlingTime time.Duration) {
	if op >= message.NumOps {
		// Only defined ops are expected here, but an undefined op MUST NOT
		// panic the node, so resolve its labels the slow way.
		opStr := op.String()
		m.messages.WithLabelValues(opStr).Inc()
		m.messageHandlingTime.WithLabelValues(opStr).Add(float64(handlingTime))
		return
	}
	m.messagesByOp[op].Inc()
	m.messageHandlingTimeByOp[op].Add(float64(handlingTime))
}

// observeExpired records that a message with op was dropped because its
// deadline passed.
func (m *metrics) observeExpired(op message.Op) {
	if op >= message.NumOps {
		// See [metrics.observeHandled].
		m.expired.WithLabelValues(op.String()).Inc()
		return
	}
	m.expiredByOp[op].Inc()
}
