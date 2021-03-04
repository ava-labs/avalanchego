// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type metrics struct {
	numProcessing            prometheus.Gauge
	latAccepted, latRejected prometheus.Histogram
	log                      logging.Logger

	clock         timer.Clock
	processingTxs *ProcessingTxs
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(log logging.Logger, namespace string, registerer prometheus.Registerer) error {
	m.processingTxs = NewProcessingTxs()
	m.log = log

	m.numProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "processing",
		Help:      "Number of currently processing blocks",
	})
	m.latAccepted = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "accepted",
		Help:      "Latency of accepting from the time the block was issued in milliseconds",
		Buckets:   timer.MillisecondsBuckets,
	})
	m.latRejected = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "rejected",
		Help:      "Latency of rejecting from the time the block was issued in milliseconds",
		Buckets:   timer.MillisecondsBuckets,
	})

	if err := registerer.Register(m.numProcessing); err != nil {
		return fmt.Errorf("failed to register processing statistics due to %w", err)
	}
	if err := registerer.Register(m.latAccepted); err != nil {
		return fmt.Errorf("failed to register accepted statistics due to %w", err)
	}
	if err := registerer.Register(m.latRejected); err != nil {
		return fmt.Errorf("failed to register rejected statistics due to %w", err)
	}
	return nil
}

func (m *metrics) Issued(id ids.ID) {
	m.processingTxs.PutTx(id, m.clock.Time())
	m.numProcessing.Inc()
}

func (m *metrics) Accepted(id ids.ID) {
	start, ok := m.processingTxs.GetTx(id)
	if !ok {
		m.log.Debug("unable to measure Accepted transaction %v", id.String())
		return
	}
	end := m.clock.Time()

	m.processingTxs.Evict(id)

	m.latAccepted.Observe(float64(end.Sub(start.Time).Milliseconds()))
	m.numProcessing.Dec()
}

func (m *metrics) Rejected(id ids.ID) {
	start, ok := m.processingTxs.GetTx(id)
	if !ok {
		m.log.Debug("unable to measure Rejected transaction %v", id.String())
		return
	}
	end := m.clock.Time()

	m.processingTxs.Evict(id)

	m.latRejected.Observe(float64(end.Sub(start.Time).Milliseconds()))
	m.numProcessing.Dec()
}
