// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func newCounterMetric(namespace, name string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      name,
		Help:      fmt.Sprintf("# of times a %s occurred", name),
	})
}

type metrics struct {
	get,
	put,
	evict,
	flush prometheus.Histogram

	hit,
	miss prometheus.Counter
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.get = metric.NewNanosecondsLatencyMetric(namespace, "get")
	m.put = metric.NewNanosecondsLatencyMetric(namespace, "put")
	m.evict = metric.NewNanosecondsLatencyMetric(namespace, "evict")
	m.flush = metric.NewNanosecondsLatencyMetric(namespace, "flush")
	m.hit = newCounterMetric(namespace, "hit")
	m.miss = newCounterMetric(namespace, "miss")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.get),
		registerer.Register(m.put),
		registerer.Register(m.evict),
		registerer.Register(m.flush),
		registerer.Register(m.hit),
		registerer.Register(m.miss),
	)
	return errs.Err
}
