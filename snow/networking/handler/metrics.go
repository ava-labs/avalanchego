// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"
)

type metrics struct {
	expired             *prometheus.CounterVec // op
	messages            *prometheus.CounterVec // op
	lockingTime         prometheus.Gauge
	messageHandlingTime *prometheus.GaugeVec // op
}

func newMetrics(namespace string, reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		expired: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "expired",
				Help:      "messages dropped because the deadline expired",
			},
			opLabels,
		),
		messages: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "messages",
				Help:      "messages handled",
			},
			opLabels,
		),
		messageHandlingTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "message_handling_time",
				Help:      "time spent handling messages",
			},
			opLabels,
		),
		lockingTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "locking_time",
			Help:      "time spent acquiring the context lock",
		}),
	}
	return m, utils.Err(
		reg.Register(m.expired),
		reg.Register(m.messages),
		reg.Register(m.messageHandlingTime),
		reg.Register(m.lockingTime),
	)
}
