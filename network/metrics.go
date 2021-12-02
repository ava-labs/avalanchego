// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type messageMetrics struct {
	receivedBytes, sentBytes, numSent, numFailed, numReceived prometheus.Counter
	savedReceivedBytes, savedSentBytes                        metric.Averager
}

func newMessageMetrics(op message.Op, namespace string, metrics prometheus.Registerer, errs *wrappers.Errs) *messageMetrics {
	msg := &messageMetrics{
		numSent: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_sent", op),
			Help:      fmt.Sprintf("Number of %s messages sent over the network", op),
		}),
		numFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_failed", op),
			Help:      fmt.Sprintf("Number of %s messages that failed to be sent over the network", op),
		}),
		numReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_received", op),
			Help:      fmt.Sprintf("Number of %s messages received from the network", op),
		}),
		receivedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_received_bytes", op),
			Help:      fmt.Sprintf("Number of bytes of %s messages received from the network", op),
		}),
		sentBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_sent_bytes", op),
			Help:      fmt.Sprintf("Size of bytes of %s messages received from the network", op),
		}),
	}
	errs.Add(
		metrics.Register(msg.numSent),
		metrics.Register(msg.numFailed),
		metrics.Register(msg.numReceived),
		metrics.Register(msg.receivedBytes),
		metrics.Register(msg.sentBytes),
	)

	if op.Compressable() {
		msg.savedReceivedBytes = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compression_saved_received_bytes", op),
			fmt.Sprintf("bytes saved (not received) due to compression of %s messages", op),
			metrics,
			errs,
		)
		msg.savedSentBytes = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compression_saved_sent_bytes", op),
			fmt.Sprintf("bytes saved (not sent) due to compression of %s messages", op),
			metrics,
			errs,
		)
	} else {
		msg.savedReceivedBytes = metric.NewNoAverager()
		msg.savedSentBytes = metric.NewNoAverager()
	}
	return msg
}

type metrics struct {
	numPeers                  prometheus.Gauge
	timeSinceLastMsgSent      prometheus.Gauge
	timeSinceLastMsgReceived  prometheus.Gauge
	sendQueuePortionFull      prometheus.Gauge
	sendFailRate              prometheus.Gauge
	failedToParse             prometheus.Counter
	connected                 prometheus.Counter
	disconnected              prometheus.Counter
	inboundConnRateLimited    prometheus.Counter
	inboundConnAllowed        prometheus.Counter
	nodeUptimeWeightedAverage prometheus.Gauge
	nodeUptimeRewardingStake  prometheus.Gauge

	messageMetrics map[message.Op]*messageMetrics
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
		Help:      "Times this node rejected an inbound connection due to rate-limiting",
	})
	m.nodeUptimeWeightedAverage = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_uptime_weighted_average",
		Help:      "This node's uptime average weighted by observing peer stakes",
	})
	m.nodeUptimeRewardingStake = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_uptime_rewarding_stake",
		Help:      "The percentage of total stake which thinks this node is eligible for rewards",
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
		registerer.Register(m.nodeUptimeWeightedAverage),
		registerer.Register(m.nodeUptimeRewardingStake),
	)

	m.messageMetrics = make(map[message.Op]*messageMetrics, len(message.ExternalOps))
	for _, op := range message.ExternalOps {
		m.messageMetrics[op] = newMessageMetrics(op, namespace, registerer, &errs)
	}
	return errs.Err
}
