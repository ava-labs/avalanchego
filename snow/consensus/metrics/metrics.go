// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics reports commonly used consensus metrics.
type Metrics struct {
	// Clock gives access to the current wall clock time
	Clock timer.Clock

	// ProcessingEntries keeps track of the time that each item was issued into
	// the consensus instance. This is used to calculate the amount of time to
	// accept or reject the item.
	processingEntries linkedhashmap.LinkedHashmap

	// log reports anomalous events.
	log logging.Logger

	// numProcessing keeps track of the number of items processing
	numProcessing prometheus.Gauge

	// latAccepted tracks the number of nanoseconds that an item was processing
	// before being accepted
	latAccepted metric.Averager

	// rejected tracks the number of nanoseconds that an item was processing
	// before being rejected
	latRejected metric.Averager
}

// Initialize the metrics with the provided names.
func (m *Metrics) Initialize(metricName, descriptionName string, log logging.Logger, namespace string, reg prometheus.Registerer) error {
	m.processingEntries = linkedhashmap.New()
	m.log = log

	m.numProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_processing", metricName),
		Help:      fmt.Sprintf("Number of currently processing %s", metricName),
	})

	errs := wrappers.Errs{}
	m.latAccepted = metric.NewAveragerWithErrs(
		namespace,
		fmt.Sprintf("%s_accepted", metricName),
		fmt.Sprintf("time (in ns) of accepting from the time the %s was issued", descriptionName),
		reg,
		&errs,
	)
	m.latRejected = metric.NewAveragerWithErrs(
		namespace,
		fmt.Sprintf("%s_rejected", metricName),
		fmt.Sprintf("time (in ns) of rejecting from the time the %s was issued", descriptionName),
		reg,
		&errs,
	)
	errs.Add(reg.Register(m.numProcessing))
	return errs.Err
}

// Issued marks the item as having been issued.
func (m *Metrics) Issued(id ids.ID) {
	m.processingEntries.Put(id, m.Clock.Time())
	m.numProcessing.Inc()
}

// Accepted marks the item as having been accepted.
func (m *Metrics) Accepted(id ids.ID) {
	startTime, ok := m.processingEntries.Get(id)
	if !ok {
		m.log.Debug("unable to measure Accepted transaction %v", id.String())
		return
	}
	m.processingEntries.Delete(id)

	endTime := m.Clock.Time()
	duration := endTime.Sub(startTime.(time.Time))
	m.latAccepted.Observe(float64(duration))
	m.numProcessing.Dec()
}

// Rejected marks the item as having been rejected.
func (m *Metrics) Rejected(id ids.ID) {
	startTime, ok := m.processingEntries.Get(id)
	if !ok {
		m.log.Debug("unable to measure Rejected transaction %v", id.String())
		return
	}
	m.processingEntries.Delete(id)

	endTime := m.Clock.Time()
	duration := endTime.Sub(startTime.(time.Time))
	m.latRejected.Observe(float64(duration))
	m.numProcessing.Dec()
}

func (m *Metrics) MeasureAndGetOldestDuration() time.Duration {
	now := m.Clock.Time()
	oldestTimeIntf, exists := m.processingEntries.Oldest()
	oldestTime := now
	if exists {
		oldestTime = oldestTimeIntf.(time.Time)
	}

	duration := now.Sub(oldestTime)
	return duration
}

func (m *Metrics) ProcessingLen() int {
	return m.processingEntries.Len()
}
