// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type metrics struct {
	numProcessing            prometheus.Gauge
	latAccepted, latRejected prometheus.Histogram

	clock      timer.Clock
	processing map[[32]byte]time.Time
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(log logging.Logger, namespace string, registerer prometheus.Registerer) error {
	m.processing = make(map[[32]byte]time.Time)

	m.numProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "vtx_processing",
		Help:      "Number of currently processing vertices",
	})
	m.latAccepted = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "vtx_accepted",
		Help:      "Latency of accepting from the time the vertex was issued in milliseconds",
		Buckets:   timer.MillisecondsBuckets,
	})
	m.latRejected = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "vtx_rejected",
		Help:      "Latency of rejecting from the time the vertex was issued in milliseconds",
		Buckets:   timer.MillisecondsBuckets,
	})

	if err := registerer.Register(m.numProcessing); err != nil {
		return fmt.Errorf("failed to register vtx_processing statistics due to %w", err)
	}
	if err := registerer.Register(m.latAccepted); err != nil {
		return fmt.Errorf("failed to register vtx_accepted statistics due to %w", err)
	}
	if err := registerer.Register(m.latRejected); err != nil {
		return fmt.Errorf("failed to register vtx_rejected statistics due to %w", err)
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
