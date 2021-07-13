// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type messageMetrics struct {
	receivedBytes, sentBytes, numSent, numFailed, numReceived prometheus.Counter
}

func (mm *messageMetrics) initialize(msgType Op, registerer prometheus.Registerer) error {
	mm.numSent = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      fmt.Sprintf("%s_sent", msgType),
		Help:      fmt.Sprintf("Number of %s messages sent over the network", msgType),
	})
	mm.numFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      fmt.Sprintf("%s_failed", msgType),
		Help:      fmt.Sprintf("Number of %s messages that failed to be sent over the network", msgType),
	})
	mm.numReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      fmt.Sprintf("%s_received", msgType),
		Help:      fmt.Sprintf("Number of %s messages received from the network", msgType),
	})

	mm.receivedBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      fmt.Sprintf("%s_received_bytes", msgType),
		Help:      fmt.Sprintf("Number of bytes of %s messages received from the network", msgType),
	})
	mm.sentBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      fmt.Sprintf("%s_sent_bytes", msgType),
		Help:      fmt.Sprintf("Size of bytes of %s messages received from the network", msgType),
	})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(mm.numSent),
		registerer.Register(mm.numFailed),
		registerer.Register(mm.numReceived),
		registerer.Register(mm.receivedBytes),
		registerer.Register(mm.sentBytes),
	)

	return errs.Err
}

type metrics struct {
	numPeers                 prometheus.Gauge
	timeSinceLastMsgSent     prometheus.Gauge
	timeSinceLastMsgReceived prometheus.Gauge
	sendQueuePortionFull     prometheus.Gauge
	sendFailRate             prometheus.Gauge
	failedToParse            prometheus.Counter
	connected                prometheus.Counter
	disconnected             prometheus.Counter

	getVersion, version,
	getPeerlist, peerList,
	ping, pong,
	getAcceptedFrontier, acceptedFrontier,
	getAccepted, accepted,
	getAncestors, multiPut,
	get, put,
	pushQuery, pullQuery, chits,
	appRequest, appResponse, appGossip messageMetrics
}

func (m *metrics) initialize(registerer prometheus.Registerer) error {
	// Set up metrics
	m.numPeers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "peers",
		Help:      "Number of network peers",
	})
	m.timeSinceLastMsgReceived = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "time_since_last_msg_received",
		Help:      "Time since the last msg was received in milliseconds",
	})
	m.timeSinceLastMsgSent = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "time_since_last_msg_sent",
		Help:      "Time since the last msg was sent in milliseconds",
	})
	m.sendQueuePortionFull = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "send_queue_portion_full",
		Help:      "Percentage of use in Send Queue",
	})
	m.sendFailRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "send_fail_rate",
		Help:      "Portion of messages that recently failed to be sent over the network",
	})
	m.failedToParse = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      "msgs_failed_to_parse",
		Help:      "Number of messages that could not be parsed or were invalidly formed",
	})
	m.connected = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      "times_connected",
		Help:      "Times this node successfully completed a handshake with a peer",
	})
	m.disconnected = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: constants.PlatformName,
		Name:      "times_disconnected",
		Help:      "Times this node disconnected from a peer it had completed a handshake with",
	})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numPeers),
		registerer.Register(m.timeSinceLastMsgReceived),
		registerer.Register(m.timeSinceLastMsgSent),
		registerer.Register(m.sendQueuePortionFull),
		registerer.Register(m.sendFailRate),
		registerer.Register(m.failedToParse),
		registerer.Register(m.connected),
		registerer.Register(m.disconnected),

		m.getVersion.initialize(GetVersion, registerer),
		m.version.initialize(Version, registerer),
		m.getPeerlist.initialize(GetPeerList, registerer),
		m.peerList.initialize(PeerList, registerer),
		m.ping.initialize(Ping, registerer),
		m.pong.initialize(Pong, registerer),
		m.getAcceptedFrontier.initialize(GetAcceptedFrontier, registerer),
		m.acceptedFrontier.initialize(AcceptedFrontier, registerer),
		m.getAccepted.initialize(GetAccepted, registerer),
		m.accepted.initialize(Accepted, registerer),
		m.getAncestors.initialize(GetAncestors, registerer),
		m.multiPut.initialize(MultiPut, registerer),
		m.get.initialize(Get, registerer),
		m.put.initialize(Put, registerer),
		m.pushQuery.initialize(PushQuery, registerer),
		m.pullQuery.initialize(PullQuery, registerer),
		m.chits.initialize(Chits, registerer),
		m.appRequest.initialize(AppRequest, registerer),
		m.appResponse.initialize(AppResponse, registerer),
		m.appGossip.initialize(AppGossip, registerer),
	)
	return errs.Err
}

func (m *metrics) message(msgType Op) *messageMetrics {
	switch msgType {
	case GetVersion:
		return &m.getVersion
	case Version:
		return &m.version
	case GetPeerList:
		return &m.getPeerlist
	case PeerList:
		return &m.peerList
	case Ping:
		return &m.ping
	case Pong:
		return &m.pong
	case GetAcceptedFrontier:
		return &m.getAcceptedFrontier
	case AcceptedFrontier:
		return &m.acceptedFrontier
	case GetAccepted:
		return &m.getAccepted
	case Accepted:
		return &m.accepted
	case GetAncestors:
		return &m.getAncestors
	case MultiPut:
		return &m.multiPut
	case Get:
		return &m.get
	case Put:
		return &m.put
	case PushQuery:
		return &m.pushQuery
	case PullQuery:
		return &m.pullQuery
	case Chits:
		return &m.chits
	case AppRequest:
		return &m.appRequest
	case AppResponse:
		return &m.appResponse
	case AppGossip:
		return &m.appGossip
	default:
		return nil
	}
}
