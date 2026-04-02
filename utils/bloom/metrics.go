// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics is a collection of commonly useful metrics when using a long-lived
// bloom filter.
type Metrics struct {
	Count      prometheus.Gauge
	NumHashes  prometheus.Gauge
	NumEntries prometheus.Gauge
	MaxCount   prometheus.Gauge
	ResetCount prometheus.Counter
}

func NewMetrics(
	namespace string,
	registerer prometheus.Registerer,
) (*Metrics, error) {
	m := &Metrics{
		Count: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of additions that have been performed to the bloom",
		}),
		NumHashes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "hashes",
			Help:      "Number of hashes in the bloom",
		}),
		NumEntries: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "entries",
			Help:      "Number of bytes allocated to slots in the bloom",
		}),
		MaxCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "max_count",
			Help:      "Maximum number of additions that should be performed to the bloom before resetting",
		}),
		ResetCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "reset_count",
			Help:      "Number times the bloom has been reset",
		}),
	}
	err := errors.Join(
		registerer.Register(m.Count),
		registerer.Register(m.NumHashes),
		registerer.Register(m.NumEntries),
		registerer.Register(m.MaxCount),
		registerer.Register(m.ResetCount),
	)
	return m, err
}

// Reset the metrics to align with the provided bloom filter and max count.
func (m *Metrics) Reset(newFilter *Filter, maxCount int) {
	m.Count.Set(float64(newFilter.Count()))
	m.NumHashes.Set(float64(len(newFilter.hashSeeds)))
	m.NumEntries.Set(float64(len(newFilter.entries)))
	m.MaxCount.Set(float64(maxCount))
	m.ResetCount.Inc()
}
