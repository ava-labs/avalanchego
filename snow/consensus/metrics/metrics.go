// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Metrics reports commonly used consensus metrics.
type Metrics struct {
	// Clock gives access to the current wall clock time
	Clock timer.Clock

	// ProcessingEntries keeps track of the time that each item was issued into
	// the consensus instance. This is used to calculate the amount of time to
	// accept or reject the item.
	ProcessingEntries linkedhashmap.LinkedHashmap

	// log reports anomalous events.
	log logging.Logger

	// numProcessing keeps track of the number of items processing
	numProcessing prometheus.Gauge

	// latAccepted tracks the number of milliseconds that an item was processing
	// before being accepted
	latAccepted prometheus.Histogram

	// rejected tracks the number of milliseconds that an item was processing
	// before being rejected
	latRejected prometheus.Histogram
}

// Initialize the metrics with the provided names.
func (m *Metrics) Initialize(metricName, descriptionName string, log logging.Logger, namespace string, registerer prometheus.Registerer) error {
	m.ProcessingEntries = linkedhashmap.New()
	m.log = log

	m.numProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_processing", metricName),
		Help:      fmt.Sprintf("Number of currently processing %s", metricName),
	})
	m.latAccepted = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_accepted", metricName),
		Help:      fmt.Sprintf("Latency of accepting from the time the %s was issued in milliseconds", descriptionName),
		Buckets:   utils.MillisecondsBuckets,
	})
	m.latRejected = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_rejected", metricName),
		Help:      fmt.Sprintf("Latency of rejecting from the time the %s was issued in milliseconds", descriptionName),
		Buckets:   utils.MillisecondsBuckets,
	})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numProcessing),
		registerer.Register(m.latAccepted),
		registerer.Register(m.latRejected),
	)
	return errs.Err
}

// Issued marks the item as having been issued.
func (m *Metrics) Issued(id ids.ID) {
	m.ProcessingEntries.Put(id, m.Clock.Time())
	m.numProcessing.Inc()
}

// Accepted marks the item as having been accepted.
func (m *Metrics) Accepted(id ids.ID) {
	startTime, ok := m.ProcessingEntries.Get(id)
	if !ok {
		m.log.Debug("unable to measure Accepted transaction %v", id.String())
		return
	}
	m.ProcessingEntries.Delete(id)

	endTime := m.Clock.Time()
	duration := endTime.Sub(startTime.(time.Time))
	m.latAccepted.Observe(float64(duration.Milliseconds()))
	m.numProcessing.Dec()
}

// Rejected marks the item as having been rejected.
func (m *Metrics) Rejected(id ids.ID) {
	startTime, ok := m.ProcessingEntries.Get(id)
	if !ok {
		m.log.Debug("unable to measure Rejected transaction %v", id.String())
		return
	}
	m.ProcessingEntries.Delete(id)

	endTime := m.Clock.Time()
	duration := endTime.Sub(startTime.(time.Time))
	m.latRejected.Observe(float64(duration.Milliseconds()))
	m.numProcessing.Dec()
}
