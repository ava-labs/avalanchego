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
	m.appRequestFailed = initAverager(namespace, "app_request_falied", reg, &errs)
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

func (m *handlerMetrics) getMSGHistogram(msg constants.MsgType) metric.Averager {
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
	case constants.AppRequestMsg:
		return m.appRequest
	case constants.AppResponseMsg:
		return m.appResponse
	case constants.AppGossipMsg:
		return m.appGossip
	case constants.AppRequestFailedMsg:
		return m.appRequestFailed
	default:
		panic(fmt.Sprintf("unknown message type %s", msg))
	}
}
