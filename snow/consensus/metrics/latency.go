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
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var _ Latency = &latency{}

type Latency interface {
	// Issued marks the item as having been issued.
	Issued(id ids.ID, pollNumber uint64)

	// Accepted marks the item as having been accepted.
	Accepted(id ids.ID, pollNumber uint64)

	// Rejected marks the item as having been rejected.
	Rejected(id ids.ID, pollNumber uint64)

	// MeasureAndGetOldestDuration returns the amount of time the oldest item
	// has been processing.
	MeasureAndGetOldestDuration() time.Duration

	// NumProcessing returns the number of currently processing items.
	NumProcessing() int
}

type opStart struct {
	time       time.Time
	pollNumber uint64
}

// Latency reports commonly used consensus latency metrics.
type latency struct {
	// ProcessingEntries keeps track of the [opStart] that each item was issued
	// into the consensus instance. This is used to calculate the amount of time
	// to accept or reject the item.
	processingEntries linkedhashmap.LinkedHashmap

	// log reports anomalous events.
	log logging.Logger

	// numProcessing keeps track of the number of items processing
	numProcessing prometheus.Gauge

	// pollsAccepted tracks the number of polls that an item was in processing
	// for before being accepted
	pollsAccepted metric.Averager

	// pollsRejected tracks the number of polls that an item was in processing
	// for before being rejected
	pollsRejected metric.Averager

	// latAccepted tracks the number of nanoseconds that an item was processing
	// before being accepted
	latAccepted metric.Averager

	// rejected tracks the number of nanoseconds that an item was processing
	// before being rejected
	latRejected metric.Averager
}

// Initialize the metrics with the provided names.
func NewLatency(metricName, descriptionName string, log logging.Logger, namespace string, reg prometheus.Registerer) (Latency, error) {
	errs := wrappers.Errs{}
	l := &latency{
		processingEntries: linkedhashmap.New(),
		log:               log,
		numProcessing: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_processing", metricName),
			Help:      fmt.Sprintf("Number of currently processing %s", metricName),
		}),
		pollsAccepted: metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_polls_accepted", metricName),
			fmt.Sprintf("number of polls from issuance of a %s to its acceptance", descriptionName),
			reg,
			&errs,
		),
		pollsRejected: metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_polls_rejected", metricName),
			fmt.Sprintf("number of polls from issuance of a %s to its rejection", descriptionName),
			reg,
			&errs,
		),
		latAccepted: metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_accepted", metricName),
			fmt.Sprintf("time (in ns) from issuance of a %s to its acceptance", descriptionName),
			reg,
			&errs,
		),
		latRejected: metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_rejected", metricName),
			fmt.Sprintf("time (in ns) from issuance of a %s to its rejection", descriptionName),
			reg,
			&errs,
		),
	}
	errs.Add(reg.Register(l.numProcessing))
	return l, errs.Err
}

func (l *latency) Issued(id ids.ID, pollNumber uint64) {
	l.processingEntries.Put(id, opStart{
		time:       time.Now(),
		pollNumber: pollNumber,
	})
	l.numProcessing.Inc()
}

func (l *latency) Accepted(id ids.ID, pollNumber uint64) {
	startIntf, ok := l.processingEntries.Get(id)
	if !ok {
		l.log.Debug("unable to measure Accepted transaction %v", id.String())
		return
	}
	l.processingEntries.Delete(id)

	start := startIntf.(opStart)

	l.pollsAccepted.Observe(float64(pollNumber - start.pollNumber))

	duration := time.Since(start.time)
	l.latAccepted.Observe(float64(duration))
	l.numProcessing.Dec()
}

func (l *latency) Rejected(id ids.ID, pollNumber uint64) {
	startIntf, ok := l.processingEntries.Get(id)
	if !ok {
		l.log.Debug("unable to measure Rejected transaction %v", id.String())
		return
	}
	l.processingEntries.Delete(id)

	start := startIntf.(opStart)

	l.pollsRejected.Observe(float64(pollNumber - start.pollNumber))

	duration := time.Since(start.time)
	l.latRejected.Observe(float64(duration))
	l.numProcessing.Dec()
}

func (l *latency) MeasureAndGetOldestDuration() time.Duration {
	_, oldestTimeIntf, exists := l.processingEntries.Oldest()
	if !exists {
		return 0
	}
	oldestTime := oldestTimeIntf.(opStart).time
	return time.Since(oldestTime)
}

func (l *latency) NumProcessing() int {
	return l.processingEntries.Len()
}
