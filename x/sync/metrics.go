// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ SyncMetrics = &mockMetrics{}
	_ SyncMetrics = &metrics{}
)

type SyncMetrics interface {
	RequestFailed()
	RequestMade()
	RequestSucceeded()
}

type mockMetrics struct {
	lock              sync.Mutex
	requestsFailed    int
	requestsMade      int
	requestsSucceeded int
}

func (m *mockMetrics) RequestFailed() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.requestsFailed++
}

func (m *mockMetrics) RequestMade() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.requestsMade++
}

func (m *mockMetrics) RequestSucceeded() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.requestsSucceeded++
}

type metrics struct {
	requestsFailed    prometheus.Counter
	requestsMade      prometheus.Counter
	requestsSucceeded prometheus.Counter
}

func NewMetrics(namespace string, reg prometheus.Registerer) (SyncMetrics, error) {
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
	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.requestsFailed),
		reg.Register(m.requestsMade),
		reg.Register(m.requestsSucceeded),
	)
	return &m, errs.Err
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
