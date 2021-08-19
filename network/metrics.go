// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type messageMetrics struct {
	receivedBytes, sentBytes, numSent, numFailed, numReceived prometheus.Counter
	savedReceivedBytes, savedSentBytes                        metric.Averager
}

func (mm *messageMetrics) initialize(msgType message.Op, namespace string, metrics prometheus.Registerer) error {
	mm.numSent = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_sent", msgType),
		Help:      fmt.Sprintf("Number of %s messages sent over the network", msgType),
	})
	mm.numFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_failed", msgType),
		Help:      fmt.Sprintf("Number of %s messages that failed to be sent over the network", msgType),
	})
	mm.numReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_received", msgType),
		Help:      fmt.Sprintf("Number of %s messages received from the network", msgType),
	})
	mm.receivedBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_received_bytes", msgType),
		Help:      fmt.Sprintf("Number of bytes of %s messages received from the network", msgType),
	})
	mm.sentBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_sent_bytes", msgType),
		Help:      fmt.Sprintf("Size of bytes of %s messages received from the network", msgType),
	})

	errs := wrappers.Errs{}

	if msgType.Compressable() {
		mm.savedReceivedBytes = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compression_saved_received_bytes", msgType),
			fmt.Sprintf("bytes saved (not received) due to compression of %s messages", msgType),
			metrics,
			&errs,
		)
		mm.savedSentBytes = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compression_saved_sent_bytes", msgType),
			fmt.Sprintf("bytes saved (not sent) due to compression of %s messages", msgType),
			metrics,
			&errs,
		)
	} else {
		mm.savedReceivedBytes = metric.NewNoAverager()
		mm.savedSentBytes = metric.NewNoAverager()
	}

	errs.Add(
		metrics.Register(mm.numSent),
		metrics.Register(mm.numFailed),
		metrics.Register(mm.numReceived),
		metrics.Register(mm.receivedBytes),
		metrics.Register(mm.sentBytes),
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
	inboundConnRateLimited   prometheus.Counter
	inboundConnAllowed       prometheus.Counter

	getVersion, version,
	getPeerlist, peerList,
	ping, pong,
	getAcceptedFrontier, acceptedFrontier,
	getAccepted, accepted,
	getAncestors, multiPut,
	get, put,
	pushQuery, pullQuery, chits messageMetrics
}

func (m *metrics) initialize(namespace string, registerer prometheus.Registerer) error {
	m.numPeers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "peers",
		Help:      "Number of network peers",
	})
	m.timeSinceLastMsgReceived = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "time_since_last_msg_received",
		Help:      "Time (in ns) since the last msg was received",
	})
	m.timeSinceLastMsgSent = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "time_since_last_msg_sent",
		Help:      "Time (in ns) since the last msg was sent",
	})
	m.sendQueuePortionFull = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "send_queue_portion_full",
		Help:      "Percentage of use in Send Queue",
	})
	m.sendFailRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "send_fail_rate",
		Help:      "Portion of messages that recently failed to be sent over the network",
	})
	m.failedToParse = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "msgs_failed_to_parse",
		Help:      "Number of messages that could not be parsed or were invalidly formed",
	})
	m.connected = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "times_connected",
		Help:      "Times this node successfully completed a handshake with a peer",
	})
	m.disconnected = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "times_disconnected",
		Help:      "Times this node disconnected from a peer it had completed a handshake with",
	})
	m.inboundConnAllowed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "inbound_conn_throttler_allowed",
		Help:      "Times this node allowed (attempted to upgrade) an inbound connection",
	})
	m.inboundConnRateLimited = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "inbound_conn_throttler_rate_limited",
		Help:      "Times this node rejected an inbound connection due to rate-limiting.",
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
		registerer.Register(m.inboundConnAllowed),
		registerer.Register(m.inboundConnRateLimited),

		m.getVersion.initialize(message.GetVersion, namespace, registerer),
		m.version.initialize(message.Version, namespace, registerer),
		m.getPeerlist.initialize(message.GetPeerList, namespace, registerer),
		m.peerList.initialize(message.PeerList, namespace, registerer),
		m.ping.initialize(message.Ping, namespace, registerer),
		m.pong.initialize(message.Pong, namespace, registerer),
		m.getAcceptedFrontier.initialize(message.GetAcceptedFrontier, namespace, registerer),
		m.acceptedFrontier.initialize(message.AcceptedFrontier, namespace, registerer),
		m.getAccepted.initialize(message.GetAccepted, namespace, registerer),
		m.accepted.initialize(message.Accepted, namespace, registerer),
		m.getAncestors.initialize(message.GetAncestors, namespace, registerer),
		m.multiPut.initialize(message.MultiPut, namespace, registerer),
		m.get.initialize(message.Get, namespace, registerer),
		m.put.initialize(message.Put, namespace, registerer),
		m.pushQuery.initialize(message.PushQuery, namespace, registerer),
		m.pullQuery.initialize(message.PullQuery, namespace, registerer),
		m.chits.initialize(message.Chits, namespace, registerer),
	)
	return errs.Err
}

func (m *metrics) message(msgType message.Op) *messageMetrics {
	switch msgType {
	case message.GetVersion:
		return &m.getVersion
	case message.Version:
		return &m.version
	case message.GetPeerList:
		return &m.getPeerlist
	case message.PeerList:
		return &m.peerList
	case message.Ping:
		return &m.ping
	case message.Pong:
		return &m.pong
	case message.GetAcceptedFrontier:
		return &m.getAcceptedFrontier
	case message.AcceptedFrontier:
		return &m.acceptedFrontier
	case message.GetAccepted:
		return &m.getAccepted
	case message.Accepted:
		return &m.accepted
	case message.GetAncestors:
		return &m.getAncestors
	case message.MultiPut:
		return &m.multiPut
	case message.Get:
		return &m.get
	case message.Put:
		return &m.put
	case message.PushQuery:
		return &m.pushQuery
	case message.PullQuery:
		return &m.pullQuery
	case message.Chits:
		return &m.chits
	default:
		return nil
	}
}
