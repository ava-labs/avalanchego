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
	// for sent message, we'll use notReceive below.
	droppedLabel = "dropped"

	droppedMalformed = "malformed"
	DroppedDuplicate = "duplicate"
	droppedOther     = "other"
	droppedNot       = "not"
	notReceive       = "not_receive"
)

var (
	ioTypeDroppedLabels = []string{ioLabel, typeLabel, droppedLabel}
	sentPushLabels      = prometheus.Labels{
		ioLabel:      sentIO,
		typeLabel:    pushType,
		droppedLabel: notReceive,
	}
	sentPullLabels = prometheus.Labels{
		ioLabel:      sentIO,
		typeLabel:    pullType,
		droppedLabel: notReceive,
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
	count                   *prometheus.CounterVec
	bytes                   *prometheus.CounterVec
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
		count: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "gossip_count",
				Help:      "amount of gossip (n)",
			},
			ioTypeDroppedLabels,
		),
		bytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "gossip_bytes",
				Help:      "amount of gossip (bytes)",
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
		metrics.Register(m.count),
		metrics.Register(m.bytes),
		metrics.Register(m.tracking),
		metrics.Register(m.trackingLifetimeAverage),
		metrics.Register(m.topValidators),
	)
	return m, err
}

func (m *Metrics) observeMessage(labels prometheus.Labels, count int, bytes int) error {
	countMetric, err := m.count.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get count metric: %w", err)
	}

	bytesMetric, err := m.bytes.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get bytes metric: %w", err)
	}

	countMetric.Add(float64(count))
	bytesMetric.Add(float64(bytes))
	return nil
}

// ObserveIncomingGossipable allows external-package mempools to label a custom gossipable drop-reason and have it
// included in the gossip metrics.
func (m *Metrics) ObserveIncomingGossipable(txID ids.ID, label string) {
	m.labelsLock.Lock()
	defer m.labelsLock.Unlock()
	_, ok := m.gossipablesLabels[txID]
	if ok {
		// we already have this gossipable labeled; don't re-label.
		return
	}
	m.gossipablesLabels[txID] = label
}

// observeReceivedMessage summarizes the metrics for a given received gossip message and update the metrics accordingly.
func (m *Metrics) observeReceivedMessage(typeLabel string, gossipables map[ids.ID]int, malformedBytes int, malformedCount int) error {
	labelBytes := make(map[string]int, len(gossipables))
	labelCount := make(map[string]int, len(gossipables))

	labelBytes[droppedMalformed] = malformedBytes
	labelCount[droppedMalformed] = malformedCount

	m.labelsLock.Lock()
	for gossipable, size := range gossipables {
		if label, ok := m.gossipablesLabels[gossipable]; ok {
			labelBytes[label] += size
			labelCount[label]++
			delete(m.gossipablesLabels, gossipable)
		}
	}
	m.labelsLock.Unlock()

	for label, gossipableSize := range labelBytes {
		prometheusLabel := prometheus.Labels{
			ioLabel:      receivedIO,
			typeLabel:    typeLabel,
			droppedLabel: label,
		}
		if err := m.observeMessage(prometheusLabel, labelCount[label], gossipableSize); err != nil {
			return err
		}
	}
	return nil
}
