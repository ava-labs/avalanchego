// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var _ Latency = (*latency)(nil)

type Latency interface {
	// Issued marks the item as having been issued.
	Issued(id ids.ID, pollNumber uint64)

	// Accepted marks the item as having been accepted.
	// Pass the container size in bytes for metrics tracking.
	Accepted(id ids.ID, pollNumber uint64, containerSize int)

	// Rejected marks the item as having been rejected.
	// Pass the container size in bytes for metrics tracking.
	Rejected(id ids.ID, pollNumber uint64, containerSize int)

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
	processingEntries linkedhashmap.LinkedHashmap[ids.ID, opStart]

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
	latAccepted              metric.Averager
	containerSizeAcceptedSum prometheus.Gauge

	// rejected tracks the number of nanoseconds that an item was processing
	// before being rejected
	latRejected              metric.Averager
	containerSizeRejectedSum prometheus.Gauge
}

// Initialize the metrics with the provided names.
func NewLatency(metricName, descriptionName string, log logging.Logger, namespace string, reg prometheus.Registerer) (Latency, error) {
	errs := wrappers.Errs{}
	l := &latency{
		processingEntries: linkedhashmap.New[ids.ID, opStart](),
		log:               log,

		// e.g.,
		// "avalanche_7y7zwo7XatqnX4dtTakLo32o7jkMX4XuDa26WaxbCXoCT1qKK_blks_processing" to count how blocks are currently processing
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

		// e.g.,
		// "avalanche_C_blks_accepted_count" to count how many "Observe" gets called -- count all "Accept"
		// "avalanche_C_blks_accepted_sum" to count how many ns have elapsed since its issuance on acceptance
		// "avalanche_C_blks_accepted_sum / avalanche_C_blks_accepted_count" is the average block acceptance latency in ns
		// "avalanche_C_blks_accepted_container_size_sum" to track cumulative sum of all accepted blocks' sizes
		// "avalanche_C_blks_accepted_container_size_sum / avalanche_C_blks_accepted_count" is the average block size
		latAccepted: metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_accepted", metricName),
			fmt.Sprintf("time (in ns) from issuance of a %s to its acceptance", descriptionName),
			reg,
			&errs,
		),
		containerSizeAcceptedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_accepted_container_size_sum", metricName),
			Help:      fmt.Sprintf("Cumulative sum of container size of all accepted %s", metricName),
		}),

		// e.g.,
		// "avalanche_P_blks_rejected_count" to count how many "Observe" gets called -- count all "Reject"
		// "avalanche_P_blks_rejected_sum" to count how many ns have elapsed since its issuance on rejection
		// "avalanche_P_blks_accepted_sum / avalanche_P_blks_accepted_count" is the average block acceptance latency in ns
		// "avalanche_P_blks_accepted_container_size_sum" to track cumulative sum of all accepted blocks' sizes
		// "avalanche_P_blks_accepted_container_size_sum / avalanche_P_blks_accepted_count" is the average block size
		latRejected: metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_rejected", metricName),
			fmt.Sprintf("time (in ns) from issuance of a %s to its rejection", descriptionName),
			reg,
			&errs,
		),
		containerSizeRejectedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_rejected_container_size_sum", metricName),
			Help:      fmt.Sprintf("Cumulative sum of container size of all rejected %s", metricName),
		}),
	}
	errs.Add(
		reg.Register(l.numProcessing),
		reg.Register(l.containerSizeAcceptedSum),
		reg.Register(l.containerSizeRejectedSum),
	)
	return l, errs.Err
}

func (l *latency) Issued(id ids.ID, pollNumber uint64) {
	l.processingEntries.Put(id, opStart{
		time:       time.Now(),
		pollNumber: pollNumber,
	})
	l.numProcessing.Inc()
}

func (l *latency) Accepted(id ids.ID, pollNumber uint64, containerSize int) {
	start, ok := l.processingEntries.Get(id)
	if !ok {
		l.log.Debug("unable to measure tx latency",
			zap.Stringer("status", choices.Accepted),
			zap.Stringer("txID", id),
		)
		return
	}
	l.processingEntries.Delete(id)

	l.pollsAccepted.Observe(float64(pollNumber - start.pollNumber))

	duration := time.Since(start.time)
	l.latAccepted.Observe(float64(duration))
	l.numProcessing.Dec()

	l.containerSizeAcceptedSum.Add(float64(containerSize))
}

func (l *latency) Rejected(id ids.ID, pollNumber uint64, containerSize int) {
	start, ok := l.processingEntries.Get(id)
	if !ok {
		l.log.Debug("unable to measure tx latency",
			zap.Stringer("status", choices.Rejected),
			zap.Stringer("txID", id),
		)
		return
	}
	l.processingEntries.Delete(id)

	l.pollsRejected.Observe(float64(pollNumber - start.pollNumber))

	duration := time.Since(start.time)
	l.latRejected.Observe(float64(duration))
	l.numProcessing.Dec()

	l.containerSizeRejectedSum.Add(float64(containerSize))
}

func (l *latency) MeasureAndGetOldestDuration() time.Duration {
	_, oldestOp, exists := l.processingEntries.Oldest()
	if !exists {
		return 0
	}
	return time.Since(oldestOp.time)
}

func (l *latency) NumProcessing() int {
	return l.processingEntries.Len()
}
