// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/timer"
)

type metrics struct {
	numProcessing            prometheus.Gauge
	latAccepted, latRejected prometheus.Histogram

	clock      timer.Clock
	processing map[[32]byte]time.Time
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(namespace string, registerer prometheus.Registerer) error {
	m.processing = make(map[[32]byte]time.Time)

	m.numProcessing = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tx_processing",
			Help:      "Number of processing transactions",
		})
	m.latAccepted = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_accepted",
			Help:      "Latency of accepting from the time the transaction was issued in milliseconds",
			Buckets:   timer.MillisecondsBuckets,
		})
	m.latRejected = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_rejected",
			Help:      "Latency of rejecting from the time the transaction was issued in milliseconds",
			Buckets:   timer.MillisecondsBuckets,
		})

	if err := registerer.Register(m.numProcessing); err != nil {
		return fmt.Errorf("Failed to register tx_processing statistics due to %s", err)
	}
	if err := registerer.Register(m.latAccepted); err != nil {
		return fmt.Errorf("Failed to register tx_accepted statistics due to %s", err)
	}
	if err := registerer.Register(m.latRejected); err != nil {
		return fmt.Errorf("Failed to register tx_rejected statistics due to %s", err)
	}
	return nil
}

func (m *metrics) Issued(id ids.ID) {
	m.processing[id.Key()] = m.clock.Time()
	m.numProcessing.Inc()
}

func (m *metrics) Accepted(id ids.ID) {
	key := id.Key()
	start := m.processing[key]
	end := m.clock.Time()

	delete(m.processing, key)

	m.latAccepted.Observe(float64(end.Sub(start).Milliseconds()))
	m.numProcessing.Dec()
}

func (m *metrics) Rejected(id ids.ID) {
	key := id.Key()
	start := m.processing[key]
	end := m.clock.Time()

	delete(m.processing, key)

	m.latRejected.Observe(float64(end.Sub(start).Milliseconds()))
	m.numProcessing.Dec()
}
