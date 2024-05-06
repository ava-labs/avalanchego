// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"
)

const (
	resultLabel = "result"
	hitResult   = "hit"
	missResult  = "miss"
)

var (
	resultLabels = []string{resultLabel}
	hitLabels    = prometheus.Labels{
		resultLabel: hitResult,
	}
	missLabels = prometheus.Labels{
		resultLabel: missResult,
	}
)

type metrics struct {
	getCount *prometheus.CounterVec
	getTime  *prometheus.CounterVec

	putCount prometheus.Counter
	putTime  prometheus.Counter

	len           prometheus.Gauge
	portionFilled prometheus.Gauge
}

func newMetrics(
	namespace string,
	reg prometheus.Registerer,
) (*metrics, error) {
	m := &metrics{
		getCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "get_count",
				Help:      "number of get calls",
			},
			resultLabels,
		),
		getTime: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "get_time",
				Help:      "time spent (ns) in get calls",
			},
			resultLabels,
		),
		putCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "put_count",
			Help:      "number of put calls",
		}),
		putTime: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "put_time",
			Help:      "time spent (ns) in put calls",
		}),
		len: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "len",
			Help:      "number of entries",
		}),
		portionFilled: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "portion_filled",
			Help:      "fraction of cache filled",
		}),
	}
	return m, utils.Err(
		reg.Register(m.getCount),
		reg.Register(m.getTime),
		reg.Register(m.putCount),
		reg.Register(m.putTime),
		reg.Register(m.len),
		reg.Register(m.portionFilled),
	)
}
