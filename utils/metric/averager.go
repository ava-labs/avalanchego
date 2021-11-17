// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type Averager interface {
	Observe(float64)
}

type averager struct {
	count prometheus.Counter
	sum   prometheus.Gauge
}

func NewAverager(namespace, name, desc string, reg prometheus.Registerer) (Averager, error) {
	errs := wrappers.Errs{}
	a := NewAveragerWithErrs(namespace, name, desc, reg, &errs)
	return a, errs.Err
}

func NewAveragerWithErrs(namespace, name, desc string, reg prometheus.Registerer, errs *wrappers.Errs) Averager {
	a := averager{
		count: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_count", name),
			Help:      fmt.Sprintf("# of observations of %s", desc),
		}),
		sum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_sum", name),
			Help:      fmt.Sprintf("Sum of %s", desc),
		}),
	}

	errs.Add(
		reg.Register(a.count),
		reg.Register(a.sum),
	)
	return &a
}

func (a *averager) Observe(v float64) {
	a.count.Inc()
	a.sum.Add(v)
}

type noAverager struct{}

func NewNoAverager() Averager {
	return noAverager{}
}

func (noAverager) Observe(float64) {}
