// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	defaultRequestHelpMsg = "Time spent processing this request in nanoseconds"
)

func initHistogram(namespace, name, help string, registerer prometheus.Registerer, errs *wrappers.Errs) prometheus.Histogram {
	histogram := prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      name,
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})

	if err := registerer.Register(histogram); err != nil {
		errs.Add(fmt.Errorf("failed to register %s statistics due to %s", name, err))
	}
	return histogram
}

func initGauge(namespace, name, help string, registerer prometheus.Registerer, errs *wrappers.Errs) prometheus.Gauge {
	gauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      name,
			Help:      help,
		})
	if err := registerer.Register(gauge); err != nil {
		errs.Add(fmt.Errorf("failed to register %s statistics due to %s", name, err))
	}
	return gauge
}

type metrics struct {
	namespace                   string
	registerer                  prometheus.Registerer
	pending                     prometheus.Gauge
	dropped, expired, throttled prometheus.Counter
	getAcceptedFrontier, acceptedFrontier, getAcceptedFrontierFailed,
	getAccepted, accepted, getAcceptedFailed,
	getAncestors, multiPut, getAncestorsFailed,
	get, put, getFailed,
	pushQuery, pullQuery, chits, queryFailed,
	notify,
	gossip,
	cpu,
	shutdown prometheus.Histogram
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(namespace string, registerer prometheus.Registerer) error {
	m.namespace = namespace
	m.registerer = registerer
	errs := wrappers.Errs{}

	m.pending = initGauge(namespace, "pending", "Number of pending events", registerer, &errs)

	m.dropped = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "dropped",
			Help:      "Number of dropped events",
		})

	if err := registerer.Register(m.dropped); err != nil {
		errs.Add(fmt.Errorf("failed to register dropped statistics due to %s", err))
	}

	m.expired = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "expired",
			Help:      "Number of expired events",
		})

	if err := registerer.Register(m.expired); err != nil {
		errs.Add(fmt.Errorf("failed to register expired statistics due to %s", err))
	}

	m.throttled = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "throttled",
			Help:      "Number of throttled events",
		})

	if err := registerer.Register(m.throttled); err != nil {
		errs.Add(fmt.Errorf("failed to register throttled statistics due to %s", err))
	}

	m.cpu = initHistogram(
		namespace,
		"cpu_utilization",
		"Time this handler's engine spent processing messages for a single CPU interval",
		registerer,
		&errs,
	)
	m.getAcceptedFrontier = initHistogram(namespace, "get_accepted_frontier", defaultRequestHelpMsg, registerer, &errs)
	m.acceptedFrontier = initHistogram(namespace, "accepted_frontier", defaultRequestHelpMsg, registerer, &errs)
	m.getAcceptedFrontierFailed = initHistogram(namespace, "get_accepted_frontier_failed", defaultRequestHelpMsg, registerer, &errs)
	m.getAccepted = initHistogram(namespace, "get_accepted", defaultRequestHelpMsg, registerer, &errs)
	m.accepted = initHistogram(namespace, "accepted", defaultRequestHelpMsg, registerer, &errs)
	m.getAcceptedFailed = initHistogram(namespace, "get_accepted_failed", defaultRequestHelpMsg, registerer, &errs)
	m.getAncestors = initHistogram(namespace, "get_ancestors", defaultRequestHelpMsg, registerer, &errs)
	m.multiPut = initHistogram(namespace, "multi_put", defaultRequestHelpMsg, registerer, &errs)
	m.getAncestorsFailed = initHistogram(namespace, "get_ancestors_failed", defaultRequestHelpMsg, registerer, &errs)
	m.get = initHistogram(namespace, "get", defaultRequestHelpMsg, registerer, &errs)
	m.put = initHistogram(namespace, "put", defaultRequestHelpMsg, registerer, &errs)
	m.getFailed = initHistogram(namespace, "get_failed", defaultRequestHelpMsg, registerer, &errs)
	m.pushQuery = initHistogram(namespace, "push_query", defaultRequestHelpMsg, registerer, &errs)
	m.pullQuery = initHistogram(namespace, "pull_query", defaultRequestHelpMsg, registerer, &errs)
	m.chits = initHistogram(namespace, "chits", defaultRequestHelpMsg, registerer, &errs)
	m.queryFailed = initHistogram(namespace, "query_failed", defaultRequestHelpMsg, registerer, &errs)
	m.notify = initHistogram(namespace, "notify", defaultRequestHelpMsg, registerer, &errs)
	m.gossip = initHistogram(namespace, "gossip", defaultRequestHelpMsg, registerer, &errs)
	m.shutdown = initHistogram(namespace, "shutdown", "Time spent in the process of shutting down", registerer, &errs)

	return errs.Err
}

func (m *metrics) registerTierStatistics(tier int) (prometheus.Gauge, prometheus.Histogram, error) {
	errs := wrappers.Errs{}

	gauge := initGauge(
		m.namespace,
		fmt.Sprintf("tier_%d", tier),
		fmt.Sprintf("Number of pending messages on tier %d of the multi-level message queue", tier),
		m.registerer,
		&errs,
	)
	histogram := initHistogram(
		m.namespace,
		fmt.Sprintf("tier_%d_wait_time", tier),
		fmt.Sprintf("Amount of time a message waits on tier %d queue before being processed", tier),
		m.registerer,
		&errs,
	)
	return gauge, histogram, errs.Err
}
