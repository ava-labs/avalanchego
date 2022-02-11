// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	// failingChecks keeps track of the number of check failing
	failingChecks prometheus.Gauge
}

func newMetrics(namespace string, registerer prometheus.Registerer) (*metrics, error) {
	metrics := &metrics{
		failingChecks: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "checks_failing",
			Help:      "number of currently failing health checks",
		}),
	}
	return metrics, registerer.Register(metrics.failingChecks)
}
