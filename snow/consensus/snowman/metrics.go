// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type metrics struct {
	numProcessing            prometheus.Gauge
	latAccepted, latRejected prometheus.Histogram
	log                      logging.Logger

	clock          timer.Clock
	processingBlks linkedhashmap.LinkedHashmap
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(log logging.Logger, namespace string, registerer prometheus.Registerer) error {
	m.processingBlks = linkedhashmap.New()
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
	m.processingBlks.Put(id, m.clock.Time())
	m.numProcessing.Inc()
}

func (m *metrics) Accepted(id ids.ID) {
	startTime, ok := m.processingBlks.Get(id)
	if !ok {
		m.log.Debug("unable to measure Accepted block %v", id.String())
		return
	}
	m.processingBlks.Delete(id)

	endTime := m.clock.Time()
	duration := endTime.Sub(startTime.(time.Time))
	m.latAccepted.Observe(float64(duration.Milliseconds()))
	m.numProcessing.Dec()
}

func (m *metrics) Rejected(id ids.ID) {
	startTime, ok := m.processingBlks.Get(id)
	if !ok {
		m.log.Debug("unable to measure Rejected block %v", id.String())
		return
	}
	m.processingBlks.Delete(id)

	endTime := m.clock.Time()
	duration := endTime.Sub(startTime.(time.Time))
	m.latRejected.Observe(float64(duration.Milliseconds()))
	m.numProcessing.Dec()
}
