// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

type opStart struct {
	time       time.Time
	pollNumber uint64
}

// Latency reports commonly used consensus latency metrics.
type Latency struct {
	// Clock gives access to the current wall clock time
	Clock mockable.Clock

	// ProcessingEntries keeps track of the [opStart] that each item was issued
	// into the consensus instance. This is used to calculate the amount of time
	// to accept or reject the item.
	processingEntries linkedhashmap.LinkedHashmap

	// log reports anomalous events.
	log logging.Logger

	// pollsAccepted tracks the number of polls that an item was in processing
	// for before being accepted
	pollsAccepted metric.Averager

	// pollsRejected tracks the number of polls that an item was in processing
	// for before being rejected
	pollsRejected metric.Averager

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
func (m *Latency) Initialize(metricName, descriptionName string, log logging.Logger, namespace string, reg prometheus.Registerer) error {
	m.processingEntries = linkedhashmap.New()
	m.log = log

	m.numProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_processing", metricName),
		Help:      fmt.Sprintf("Number of currently processing %s", metricName),
	})

	errs := wrappers.Errs{}
	m.pollsAccepted = metric.NewAveragerWithErrs(
		namespace,
		fmt.Sprintf("%s_polls_accepted", metricName),
		fmt.Sprintf("number of polls from issuance of a %s to its acceptance", descriptionName),
		reg,
		&errs,
	)
	m.pollsRejected = metric.NewAveragerWithErrs(
		namespace,
		fmt.Sprintf("%s_polls_rejected", metricName),
		fmt.Sprintf("number of polls from issuance of a %s to its rejection", descriptionName),
		reg,
		&errs,
	)
	m.latAccepted = metric.NewAveragerWithErrs(
		namespace,
		fmt.Sprintf("%s_accepted", metricName),
		fmt.Sprintf("time (in ns) from issuance of a %s to its acceptance", descriptionName),
		reg,
		&errs,
	)
	m.latRejected = metric.NewAveragerWithErrs(
		namespace,
		fmt.Sprintf("%s_rejected", metricName),
		fmt.Sprintf("time (in ns) from issuance of a %s to its rejection", descriptionName),
		reg,
		&errs,
	)

	errs.Add(reg.Register(m.numProcessing))
	return errs.Err
}

// Issued marks the item as having been issued.
func (m *Latency) Issued(id ids.ID, pollNumber uint64) {
	m.processingEntries.Put(id, opStart{
		time:       m.Clock.Time(),
		pollNumber: pollNumber,
	})
	m.numProcessing.Inc()
}

// Accepted marks the item as having been accepted.
func (m *Latency) Accepted(id ids.ID, pollNumber uint64) {
	startIntf, ok := m.processingEntries.Get(id)
	if !ok {
		m.log.Debug("unable to measure Accepted transaction %v", id.String())
		return
	}
	m.processingEntries.Delete(id)

	start := startIntf.(opStart)

	m.pollsAccepted.Observe(float64(pollNumber - start.pollNumber))

	endTime := m.Clock.Time()
	duration := endTime.Sub(start.time)
	m.latAccepted.Observe(float64(duration))
	m.numProcessing.Dec()
}

// Rejected marks the item as having been rejected.
func (m *Latency) Rejected(id ids.ID, pollNumber uint64) {
	startIntf, ok := m.processingEntries.Get(id)
	if !ok {
		m.log.Debug("unable to measure Rejected transaction %v", id.String())
		return
	}
	m.processingEntries.Delete(id)

	start := startIntf.(opStart)

	m.pollsRejected.Observe(float64(pollNumber - start.pollNumber))

	endTime := m.Clock.Time()
	duration := endTime.Sub(start.time)
	m.latRejected.Observe(float64(duration))
	m.numProcessing.Dec()
}

func (m *Latency) MeasureAndGetOldestDuration() time.Duration {
	now := m.Clock.Time()
	_, oldestTimeIntf, exists := m.processingEntries.Oldest()
	oldestTime := now
	if exists {
		oldestTime = oldestTimeIntf.(opStart).time
	}

	duration := now.Sub(oldestTime)
	return duration
}

func (m *Latency) ProcessingLen() int {
	return m.processingEntries.Len()
}
