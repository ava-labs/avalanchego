// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sharedconsensus

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	// numProcessing keeps track of the number of items processing
	numProcessing prometheus.Gauge

	// latAccepted tracks the number of milliseconds that an item was
	// processing before being accepted
	latAccepted prometheus.Histogram

	// rejected tracks the number of milliseconds that an item was
	// processing before being rejected
	latRejected prometheus.Histogram
	log         logging.Logger

	// Clock gives access to the current wall clock time
	Clock timer.Clock

	// ProcessingEntries keeps track of the time that each transaction was issued into
	// the consensus instance. This is used to calculate the amount of time to
	// accept or reject the item
	ProcessingEntries *ProcessingEntries
}

// Initialize implements the Engine interface
func (m *Metrics) Initialize(shortName, unitName string, log logging.Logger, namespace string, registerer prometheus.Registerer) error {
	m.ProcessingEntries = NewProcessingEntries()
	m.log = log

	m.numProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_processing", shortName),
		Help:      fmt.Sprintf("Number of currently processing %s", unitName),
	})
	m.latAccepted = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_accepted", shortName),
		Help:      fmt.Sprintf("Latency of accepting from the time the %s was issued in milliseconds", unitName),
		Buckets:   timer.MillisecondsBuckets,
	})
	m.latRejected = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_rejected", shortName),
		Help:      fmt.Sprintf("Latency of rejecting from the time the %s was issued in milliseconds", unitName),
		Buckets:   timer.MillisecondsBuckets,
	})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numProcessing),
		registerer.Register(m.latAccepted),
		registerer.Register(m.latRejected),
	)
	return errs.Err
}

func (m *Metrics) Issued(id ids.ID) {
	m.ProcessingEntries.PutEntry(id, m.Clock.Time())
	m.numProcessing.Inc()
}

func (m *Metrics) Accepted(id ids.ID) {
	start, ok := m.ProcessingEntries.GetEntry(id)
	if !ok {
		m.log.Debug("unable to measure Accepted transaction %v", id.String())
		return
	}

	end := m.Clock.Time()

	m.ProcessingEntries.Evict(id)

	m.latAccepted.Observe(float64(end.Sub(start.Time).Milliseconds()))
	m.numProcessing.Dec()
}

func (m *Metrics) Rejected(id ids.ID) {
	start, ok := m.ProcessingEntries.GetEntry(id)
	if !ok {
		m.log.Debug("unable to measure Rejected transaction %v", id.String())
		return
	}
	end := m.Clock.Time()

	m.ProcessingEntries.Evict(id)

	m.latRejected.Observe(float64(end.Sub(start.Time).Milliseconds()))
	m.numProcessing.Dec()
}
