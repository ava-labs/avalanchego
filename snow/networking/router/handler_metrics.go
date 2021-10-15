// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func initAverager(namespace, name string, reg prometheus.Registerer, errs *wrappers.Errs) metric.Averager {
	return metric.NewAveragerWithErrs(
		namespace,
		name,
		fmt.Sprintf("time (in ns) of processing a %s", name),
		reg,
		errs,
	)
}

type handlerMetrics struct {
	namespace  string
	registerer prometheus.Registerer
	expired    prometheus.Counter
	getAcceptedFrontier, acceptedFrontier, getAcceptedFrontierFailed,
	getAccepted, accepted, getAcceptedFailed,
	getAncestors, multiPut, getAncestorsFailed,
	get, put, getFailed,
	pushQuery, pullQuery, chits, queryFailed,
	appRequest, appResponse, appRequestFailed, appGossip,
	connected, disconnected,
	timeout,
	notify,
	gossip,
	shutdown metric.Averager
}

// Initialize implements the Engine interface
func (m *handlerMetrics) Initialize(namespace string, reg prometheus.Registerer) error {
	m.namespace = namespace
	m.registerer = reg
	errs := wrappers.Errs{}

	m.expired = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "expired",
		Help:      "Incoming messages dropped because the message deadline expired",
	})
	errs.Add(reg.Register(m.expired))

	m.getAcceptedFrontier = initAverager(namespace, "get_accepted_frontier", reg, &errs)
	m.acceptedFrontier = initAverager(namespace, "accepted_frontier", reg, &errs)
	m.getAcceptedFrontierFailed = initAverager(namespace, "get_accepted_frontier_failed", reg, &errs)
	m.getAccepted = initAverager(namespace, "get_accepted", reg, &errs)
	m.accepted = initAverager(namespace, "accepted", reg, &errs)
	m.getAcceptedFailed = initAverager(namespace, "get_accepted_failed", reg, &errs)
	m.getAncestors = initAverager(namespace, "get_ancestors", reg, &errs)
	m.multiPut = initAverager(namespace, "multi_put", reg, &errs)
	m.getAncestorsFailed = initAverager(namespace, "get_ancestors_failed", reg, &errs)
	m.get = initAverager(namespace, "get", reg, &errs)
	m.put = initAverager(namespace, "put", reg, &errs)
	m.getFailed = initAverager(namespace, "get_failed", reg, &errs)
	m.pushQuery = initAverager(namespace, "push_query", reg, &errs)
	m.pullQuery = initAverager(namespace, "pull_query", reg, &errs)
	m.chits = initAverager(namespace, "chits", reg, &errs)
	m.queryFailed = initAverager(namespace, "query_failed", reg, &errs)
	m.appRequest = initAverager(namespace, "app_request", reg, &errs)
	m.appResponse = initAverager(namespace, "app_response", reg, &errs)
	m.appRequestFailed = initAverager(namespace, "app_request_failed", reg, &errs)
	m.appGossip = initAverager(namespace, "app_gossip", reg, &errs)
	m.connected = initAverager(namespace, "connected", reg, &errs)
	m.disconnected = initAverager(namespace, "disconnected", reg, &errs)
	m.timeout = initAverager(namespace, "timeout", reg, &errs)
	m.notify = initAverager(namespace, "notify", reg, &errs)
	m.gossip = initAverager(namespace, "gossip", reg, &errs)

	m.shutdown = metric.NewAveragerWithErrs(
		namespace,
		"shutdown",
		"time (in ns) spent in the process of shutting down",
		reg,
		&errs,
	)

	return errs.Err
}

func (m *handlerMetrics) getMSGHistogram(msg message.Op) metric.Averager {
	switch msg {
	case message.GetAcceptedFrontier:
		return m.getAcceptedFrontier
	case message.AcceptedFrontier:
		return m.acceptedFrontier
	case message.GetAcceptedFrontierFailed:
		return m.getAcceptedFrontierFailed
	case message.GetAccepted:
		return m.getAccepted
	case message.Accepted:
		return m.accepted
	case message.GetAcceptedFailed:
		return m.getAcceptedFailed
	case message.GetAncestors:
		return m.getAncestors
	case message.GetAncestorsFailed:
		return m.getAncestorsFailed
	case message.MultiPut:
		return m.multiPut
	case message.Timeout:
		return m.timeout
	case message.Get:
		return m.get
	case message.GetFailed:
		return m.getFailed
	case message.Put:
		return m.put
	case message.PushQuery:
		return m.pushQuery
	case message.PullQuery:
		return m.pullQuery
	case message.QueryFailed:
		return m.queryFailed
	case message.Chits:
		return m.chits
	case message.Connected:
		return m.connected
	case message.Disconnected:
		return m.disconnected
	case message.AppRequest:
		return m.appRequest
	case message.AppResponse:
		return m.appResponse
	case message.AppGossip:
		return m.appGossip
	case message.AppRequestFailed:
		return m.appRequestFailed
	default:
		panic(fmt.Sprintf("unknown message type %s", msg))
	}
}
