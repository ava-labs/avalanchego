// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func newAveragerMetric(namespace, name string, reg prometheus.Registerer, errs *wrappers.Errs) metric.Averager {
	return metric.NewAveragerWithErrs(
		namespace,
		name,
		fmt.Sprintf("time (in ns) of a %s", name),
		reg,
		errs,
	)
}

func newCounterMetric(namespace, name string, reg prometheus.Registerer, errs *wrappers.Errs) prometheus.Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      name,
		Help:      fmt.Sprintf("# of times a %s occurred", name),
	})
	errs.Add(reg.Register(c))
	return c
}

type metrics struct {
	get,
	put metric.Averager

	hit,
	miss prometheus.Counter
}

func (m *metrics) Initialize(
	namespace string,
	reg prometheus.Registerer,
) error {
	errs := wrappers.Errs{}
	m.get = newAveragerMetric(namespace, "get", reg, &errs)
	m.put = newAveragerMetric(namespace, "put", reg, &errs)
	m.hit = newCounterMetric(namespace, "hit", reg, &errs)
	m.miss = newCounterMetric(namespace, "miss", reg, &errs)
	return errs.Err
}
