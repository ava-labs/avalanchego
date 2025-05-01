// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	ioLabel    = "io"
	sentIO     = "sent"
	receivedIO = "received"

	typeLabel  = "type"
	pushType   = "push"
	pullType   = "pull"
	unsentType = "unsent"
	sentType   = "sent"

	// dropped indicate that the gossipable element was not added to the set due to some reason.
	droppedLabel = "dropped"

	droppedMalformed = "malformed"
	DroppedDuplicate = "duplicate"
	droppedNot       = "not"
)

var (
	ioTypeLabels        = []string{ioLabel, typeLabel}
	ioTypeDroppedLabels = []string{ioLabel, typeLabel, droppedLabel}
	sentPushLabels      = prometheus.Labels{
		ioLabel:   sentIO,
		typeLabel: pushType,
	}
	sentPullLabels = prometheus.Labels{
		ioLabel:   sentIO,
		typeLabel: pullType,
	}
	typeLabels   = []string{typeLabel}
	unsentLabels = prometheus.Labels{
		typeLabel: unsentType,
	}
	sentLabels = prometheus.Labels{
		typeLabel: sentType,
	}
)

// Metrics that are tracked across a gossip protocol. A given protocol should
// only use a single instance of Metrics.
type Metrics struct {
	sentCount               *prometheus.CounterVec
	sentBytes               *prometheus.CounterVec
	receivedCount           *prometheus.CounterVec
	receivedBytes           *prometheus.CounterVec
	tracking                *prometheus.GaugeVec
	trackingLifetimeAverage prometheus.Gauge
	topValidators           *prometheus.GaugeVec

	labelsLock        sync.Mutex
	gossipablesLabels map[ids.ID]string
}

// NewMetrics returns a common set of metrics
func NewMetrics(
	metrics prometheus.Registerer,
	namespace string,
) (*Metrics, error) {
	m := &Metrics{
		sentCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "sent_gossip_count",
				Help:      "amount of sent gossip (n)",
			},
			ioTypeLabels,
		),
		sentBytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "sent_gossip_bytes",
				Help:      "amount of sent gossip (bytes)",
			},
			ioTypeLabels,
		),
		receivedCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "received_gossip_count",
				Help:      "amount of received gossip (n)",
			},
			ioTypeDroppedLabels,
		),
		receivedBytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "received_gossip_bytes",
				Help:      "amount of received gossip (bytes)",
			},
			ioTypeDroppedLabels,
		),
		tracking: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "gossip_tracking",
				Help:      "number of gossipables being tracked",
			},
			typeLabels,
		),
		trackingLifetimeAverage: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gossip_tracking_lifetime_average",
			Help:      "average duration a gossipable has been tracked (ns)",
		}),
		topValidators: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "top_validators",
				Help:      "number of validators gossipables are sent to due to stake",
			},
			typeLabels,
		),
		gossipablesLabels: make(map[ids.ID]string),
	}
	err := errors.Join(
		metrics.Register(m.sentCount),
		metrics.Register(m.sentBytes),
		metrics.Register(m.receivedCount),
		metrics.Register(m.receivedBytes),
		metrics.Register(m.tracking),
		metrics.Register(m.trackingLifetimeAverage),
		metrics.Register(m.topValidators),
	)
	return m, err
}

// updateMetrics update the provided metric counters after matching them with the provided labels.
func updateMetrics(gossipableCounter *prometheus.CounterVec, gossipableBytes *prometheus.CounterVec, labels prometheus.Labels, count int, bytes int) error {
	countMetric, err := gossipableCounter.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get count metric: %w", err)
	}

	bytesMetric, err := gossipableBytes.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get bytes metric: %w", err)
	}

	countMetric.Add(float64(count))
	bytesMetric.Add(float64(bytes))
	return nil
}

func (m *Metrics) updateSentMetrics(labels prometheus.Labels, count int, bytes int) error {
	return updateMetrics(m.sentCount, m.sentBytes, labels, count, bytes)
}

// AddDropMetric allows external-package mempools to label a custom gossipable drop-reason and have it
// included in the gossip metrics.
func (m *Metrics) AddDropMetric(gossipableID ids.ID, label string) {
	m.labelsLock.Lock()
	defer m.labelsLock.Unlock()
	m.gossipablesLabels[gossipableID] = label
}

// updateReceivedMetrics summarizes the metrics for a given received gossip message and update the metrics accordingly.
func (m *Metrics) updateReceivedMetrics(messageType string, gossipables map[ids.ID]int, malformedBytes int, malformedCount int) error {
	m.labelsLock.Lock()
	defer m.labelsLock.Unlock()

	labelBytes := make(map[string]int, len(gossipables))
	labelCount := make(map[string]int, len(gossipables))

	labelBytes[droppedMalformed] = malformedBytes
	labelCount[droppedMalformed] = malformedCount

	for gossipable, size := range gossipables {
		if label, ok := m.gossipablesLabels[gossipable]; ok {
			labelBytes[label] += size
			labelCount[label]++
			delete(m.gossipablesLabels, gossipable)
		}
	}

	for label, gossipableSize := range labelBytes {
		prometheusLabel := prometheus.Labels{
			ioLabel:      receivedIO,
			typeLabel:    messageType,
			droppedLabel: label,
		}
		if err := updateMetrics(m.receivedCount, m.receivedBytes, prometheusLabel, labelCount[label], gossipableSize); err != nil {
			return err
		}
	}
	return nil
}
