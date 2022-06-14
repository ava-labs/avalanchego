// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numTracked                prometheus.Gauge
	numPeers                  prometheus.Gauge
	numSubnetPeers            *prometheus.GaugeVec
	timeSinceLastMsgSent      prometheus.Gauge
	timeSinceLastMsgReceived  prometheus.Gauge
	sendQueuePortionFull      prometheus.Gauge
	sendFailRate              prometheus.Gauge
	connected                 prometheus.Counter
	disconnected              prometheus.Counter
	inboundConnRateLimited    prometheus.Counter
	inboundConnAllowed        prometheus.Counter
	nodeUptimeWeightedAverage prometheus.Gauge
	nodeUptimeRewardingStake  prometheus.Gauge
}

func newMetrics(namespace string, registerer prometheus.Registerer, initialSubnetIDs ids.Set) (*metrics, error) {
	m := &metrics{
		numPeers: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "peers",
			Help:      "Number of network peers",
		}),
		numTracked: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tracked",
			Help:      "Number of currently tracked IPs attempting to be connected to",
		}),
		numSubnetPeers: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "peers_subnet",
				Help:      "Number of peers that are validating a particular subnet",
			},
			[]string{"subnetID"},
		),
		timeSinceLastMsgReceived: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "time_since_last_msg_received",
			Help:      "Time (in ns) since the last msg was received",
		}),
		timeSinceLastMsgSent: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "time_since_last_msg_sent",
			Help:      "Time (in ns) since the last msg was sent",
		}),
		sendQueuePortionFull: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "send_queue_portion_full",
			Help:      "Percentage of use in Send Queue",
		}),
		sendFailRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "send_fail_rate",
			Help:      "Portion of messages that recently failed to be sent over the network",
		}),
		connected: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "times_connected",
			Help:      "Times this node successfully completed a handshake with a peer",
		}),
		disconnected: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "times_disconnected",
			Help:      "Times this node disconnected from a peer it had completed a handshake with",
		}),
		inboundConnAllowed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "inbound_conn_throttler_allowed",
			Help:      "Times this node allowed (attempted to upgrade) an inbound connection",
		}),
		inboundConnRateLimited: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "inbound_conn_throttler_rate_limited",
			Help:      "Times this node rejected an inbound connection due to rate-limiting",
		}),
		nodeUptimeWeightedAverage: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "node_uptime_weighted_average",
			Help:      "This node's uptime average weighted by observing peer stakes",
		}),
		nodeUptimeRewardingStake: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "node_uptime_rewarding_stake",
			Help:      "The percentage of total stake which thinks this node is eligible for rewards",
		}),
	}

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numTracked),
		registerer.Register(m.numPeers),
		registerer.Register(m.numSubnetPeers),
		registerer.Register(m.timeSinceLastMsgReceived),
		registerer.Register(m.timeSinceLastMsgSent),
		registerer.Register(m.sendQueuePortionFull),
		registerer.Register(m.sendFailRate),
		registerer.Register(m.connected),
		registerer.Register(m.disconnected),
		registerer.Register(m.inboundConnAllowed),
		registerer.Register(m.inboundConnRateLimited),
		registerer.Register(m.nodeUptimeWeightedAverage),
		registerer.Register(m.nodeUptimeRewardingStake),
	)

	// init subnet tracker metrics with whitelisted subnets
	for subnetID := range initialSubnetIDs {
		// no need to track primary network ID
		if subnetID == constants.PrimaryNetworkID {
			continue
		}
		// initialize to 0
		m.numSubnetPeers.WithLabelValues(subnetID.String()).Set(0)
	}
	return m, errs.Err
}

func (m *metrics) markConnected(peer peer.Peer) {
	m.numPeers.Inc()
	m.connected.Inc()

	trackedSubnets := peer.TrackedSubnets()
	for subnetID := range trackedSubnets {
		m.numSubnetPeers.WithLabelValues(subnetID.String()).Inc()
	}
}

func (m *metrics) markDisconnected(peer peer.Peer) {
	m.numPeers.Dec()
	m.disconnected.Inc()

	trackedSubnets := peer.TrackedSubnets()
	for subnetID := range trackedSubnets {
		m.numSubnetPeers.WithLabelValues(subnetID.String()).Dec()
	}
}
