// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	expired             *prometheus.CounterVec // op
	messages            *prometheus.CounterVec // op
	lockingTime         prometheus.Gauge
	messageHandlingTime *prometheus.GaugeVec // op
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
	return m, errors.Join(
		reg.Register(m.expired),
		reg.Register(m.messages),
		reg.Register(m.messageHandlingTime),
		reg.Register(m.lockingTime),
	)
}
