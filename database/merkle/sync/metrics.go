// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ SyncMetrics = (*noopMetrics)(nil)
	_ SyncMetrics = (*metrics)(nil)
)

type SyncMetrics interface {
	RequestFailed()
	RequestMade()
	RequestSucceeded()
}

type noopMetrics struct{}

func (noopMetrics) RequestFailed()    {}
func (noopMetrics) RequestMade()      {}
func (noopMetrics) RequestSucceeded() {}

type metrics struct {
	requestsFailed    prometheus.Counter
	requestsMade      prometheus.Counter
	requestsSucceeded prometheus.Counter
}

func NewMetrics(namespace string, reg prometheus.Registerer) (SyncMetrics, error) {
	if reg == nil {
		return noopMetrics{}, nil
	}
	m := metrics{
		requestsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_failed",
			Help:      "cumulative amount of failed proof requests",
		}),
		requestsMade: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_made",
			Help:      "cumulative amount of proof requests made",
		}),
		requestsSucceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_succeeded",
			Help:      "cumulative amount of proof requests that were successful",
		}),
	}
	err := errors.Join(
		reg.Register(m.requestsFailed),
		reg.Register(m.requestsMade),
		reg.Register(m.requestsSucceeded),
	)
	return &m, err
}

func (m *metrics) RequestFailed() {
	m.requestsFailed.Inc()
}

func (m *metrics) RequestMade() {
	m.requestsMade.Inc()
}

func (m *metrics) RequestSucceeded() {
	m.requestsSucceeded.Inc()
}
