// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	defaultRequestHelpMsg = "Time spent processing this request in nanoseconds"
)

func initHistogram(namespace, name string, registerer prometheus.Registerer, errs *wrappers.Errs) prometheus.Histogram {
	histogram := prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      name,
			Help:      defaultRequestHelpMsg,
			Buckets:   metric.NanosecondsBuckets,
		})

	if err := registerer.Register(histogram); err != nil {
		errs.Add(fmt.Errorf("failed to register %s statistics due to %w", name, err))
	}
	return histogram
}

type handlerMetrics struct {
	namespace  string
	registerer prometheus.Registerer
	pending    prometheus.Gauge
	expired    prometheus.Counter
	getAcceptedFrontier, acceptedFrontier, getAcceptedFrontierFailed,
	getAccepted, accepted, getAcceptedFailed,
	getAncestors, multiPut, getAncestorsFailed,
	get, put, getFailed,
	pushQuery, pullQuery, chits, queryFailed,
	connected, disconnected,
	timeout,
	notify,
	gossip,
	cpu,
	shutdown prometheus.Histogram
}

// Initialize implements the Engine interface
func (m *handlerMetrics) Initialize(namespace string, registerer prometheus.Registerer) error {
	m.namespace = namespace
	m.registerer = registerer
	errs := wrappers.Errs{}

	m.pending = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "pending",
		Help:      "Number of pending events",
	})
	errs.Add(registerer.Register(m.pending))

	m.expired = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "expired",
		Help:      "Incoming messages dropped because the message deadline expired",
	})
	errs.Add(registerer.Register(m.expired))

	m.getAcceptedFrontier = initHistogram(namespace, "get_accepted_frontier", registerer, &errs)
	m.acceptedFrontier = initHistogram(namespace, "accepted_frontier", registerer, &errs)
	m.getAcceptedFrontierFailed = initHistogram(namespace, "get_accepted_frontier_failed", registerer, &errs)
	m.getAccepted = initHistogram(namespace, "get_accepted", registerer, &errs)
	m.accepted = initHistogram(namespace, "accepted", registerer, &errs)
	m.getAcceptedFailed = initHistogram(namespace, "get_accepted_failed", registerer, &errs)
	m.getAncestors = initHistogram(namespace, "get_ancestors", registerer, &errs)
	m.multiPut = initHistogram(namespace, "multi_put", registerer, &errs)
	m.getAncestorsFailed = initHistogram(namespace, "get_ancestors_failed", registerer, &errs)
	m.get = initHistogram(namespace, "get", registerer, &errs)
	m.put = initHistogram(namespace, "put", registerer, &errs)
	m.getFailed = initHistogram(namespace, "get_failed", registerer, &errs)
	m.pushQuery = initHistogram(namespace, "push_query", registerer, &errs)
	m.pullQuery = initHistogram(namespace, "pull_query", registerer, &errs)
	m.chits = initHistogram(namespace, "chits", registerer, &errs)
	m.queryFailed = initHistogram(namespace, "query_failed", registerer, &errs)
	m.connected = initHistogram(namespace, "connected", registerer, &errs)
	m.disconnected = initHistogram(namespace, "disconnected", registerer, &errs)
	m.timeout = initHistogram(namespace, "timeout", registerer, &errs)
	m.notify = initHistogram(namespace, "notify", registerer, &errs)
	m.gossip = initHistogram(namespace, "gossip", registerer, &errs)

	m.cpu = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "cpu_utilization",
		Help:      "Time this handler's engine spent processing messages for a single CPU interval in milliseconds",
		Buckets:   metric.MillisecondsBuckets,
	})
	errs.Add(registerer.Register(m.cpu))
	m.shutdown = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "shutdown",
		Help:      "Time spent in the process of shutting down in nanoseconds",
		Buckets:   metric.NanosecondsBuckets,
	})
	errs.Add(registerer.Register(m.shutdown))

	return errs.Err
}

func (m *handlerMetrics) getMSGHistogram(msg constants.MsgType) prometheus.Histogram {
	switch msg {
	case constants.GetAcceptedFrontierMsg:
		return m.getAcceptedFrontier
	case constants.AcceptedFrontierMsg:
		return m.acceptedFrontier
	case constants.GetAcceptedFrontierFailedMsg:
		return m.getAcceptedFrontierFailed
	case constants.GetAcceptedMsg:
		return m.getAccepted
	case constants.AcceptedMsg:
		return m.accepted
	case constants.GetAcceptedFailedMsg:
		return m.getAcceptedFailed
	case constants.GetAncestorsMsg:
		return m.getAncestors
	case constants.GetAncestorsFailedMsg:
		return m.getAncestorsFailed
	case constants.MultiPutMsg:
		return m.multiPut
	case constants.TimeoutMsg:
		return m.timeout
	case constants.GetMsg:
		return m.get
	case constants.GetFailedMsg:
		return m.getFailed
	case constants.PutMsg:
		return m.put
	case constants.PushQueryMsg:
		return m.pushQuery
	case constants.PullQueryMsg:
		return m.pullQuery
	case constants.QueryFailedMsg:
		return m.queryFailed
	case constants.ChitsMsg:
		return m.chits
	case constants.ConnectedMsg:
		return m.connected
	case constants.DisconnectedMsg:
		return m.disconnected
	default:
		panic(fmt.Sprintf("unknown message type %s", msg))
	}
}
