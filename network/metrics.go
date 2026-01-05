// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/utils/set"
)

type metrics struct {
	// trackedSubnets does not include the primary network ID
	trackedSubnets set.Set[ids.ID]

	numTracked                   prometheus.Gauge
	numPeers                     prometheus.Gauge
	numSubnetPeers               *prometheus.GaugeVec
	timeSinceLastMsgSent         prometheus.Gauge
	timeSinceLastMsgReceived     prometheus.Gauge
	sendFailRate                 prometheus.Gauge
	connected                    prometheus.Counter
	disconnected                 prometheus.Counter
	acceptFailed                 prometheus.Counter
	inboundConnRateLimited       prometheus.Counter
	inboundConnAllowed           prometheus.Counter
	tlsConnRejected              prometheus.Counter
	numUselessPeerListBytes      prometheus.Counter
	nodeUptimeWeightedAverage    prometheus.Gauge
	nodeUptimeRewardingStake     prometheus.Gauge
	peerConnectedLifetimeAverage prometheus.Gauge
	lock                         sync.RWMutex
	peerConnectedStartTimes      map[ids.NodeID]float64
	peerConnectedStartTimesSum   float64
}

func newMetrics(
	registerer prometheus.Registerer,
	trackedSubnets set.Set[ids.ID],
) (*metrics, error) {
	m := &metrics{
		trackedSubnets: trackedSubnets,
		numPeers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "peers",
			Help: "Number of network peers",
		}),
		numTracked: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tracked",
			Help: "Number of currently tracked IPs attempting to be connected to",
		}),
		numSubnetPeers: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "peers_subnet",
				Help: "Number of peers that are validating a particular subnet",
			},
			[]string{"subnetID"},
		),
		timeSinceLastMsgReceived: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "time_since_last_msg_received",
			Help: "Time (in ns) since the last msg was received",
		}),
		timeSinceLastMsgSent: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "time_since_last_msg_sent",
			Help: "Time (in ns) since the last msg was sent",
		}),
		sendFailRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "send_fail_rate",
			Help: "Portion of messages that recently failed to be sent over the network",
		}),
		connected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "times_connected",
			Help: "Times this node successfully completed a handshake with a peer",
		}),
		disconnected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "times_disconnected",
			Help: "Times this node disconnected from a peer it had completed a handshake with",
		}),
		acceptFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "accept_failed",
			Help: "Times this node's listener failed to accept an inbound connection",
		}),
		inboundConnAllowed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "inbound_conn_throttler_allowed",
			Help: "Times this node allowed (attempted to upgrade) an inbound connection",
		}),
		tlsConnRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tls_conn_rejected",
			Help: "Times this node rejected a connection due to an unsupported TLS certificate",
		}),
		numUselessPeerListBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_useless_peerlist_bytes",
			Help: "Amount of useless bytes (i.e. information about nodes we already knew/don't want to connect to) received in PeerList messages",
		}),
		inboundConnRateLimited: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "inbound_conn_throttler_rate_limited",
			Help: "Times this node rejected an inbound connection due to rate-limiting",
		}),
		nodeUptimeWeightedAverage: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "node_uptime_weighted_average",
			Help: "This node's uptime average weighted by observing peer stakes",
		}),
		nodeUptimeRewardingStake: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "node_uptime_rewarding_stake",
			Help: "The percentage of total stake which thinks this node is eligible for rewards",
		}),
		peerConnectedLifetimeAverage: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "peer_connected_duration_average",
				Help: "The average duration of all peer connections in nanoseconds",
			},
		),
		peerConnectedStartTimes: make(map[ids.NodeID]float64),
	}

	err := errors.Join(
		registerer.Register(m.numTracked),
		registerer.Register(m.numPeers),
		registerer.Register(m.numSubnetPeers),
		registerer.Register(m.timeSinceLastMsgReceived),
		registerer.Register(m.timeSinceLastMsgSent),
		registerer.Register(m.sendFailRate),
		registerer.Register(m.connected),
		registerer.Register(m.disconnected),
		registerer.Register(m.acceptFailed),
		registerer.Register(m.inboundConnAllowed),
		registerer.Register(m.tlsConnRejected),
		registerer.Register(m.numUselessPeerListBytes),
		registerer.Register(m.inboundConnRateLimited),
		registerer.Register(m.nodeUptimeWeightedAverage),
		registerer.Register(m.nodeUptimeRewardingStake),
		registerer.Register(m.peerConnectedLifetimeAverage),
	)

	// init subnet tracker metrics with tracked subnets
	for subnetID := range trackedSubnets {
		// initialize to 0
		subnetIDStr := subnetID.String()
		m.numSubnetPeers.WithLabelValues(subnetIDStr).Set(0)
	}

	return m, err
}

func (m *metrics) markConnected(peer peer.Peer) {
	m.numPeers.Inc()
	m.connected.Inc()

	trackedSubnets := peer.TrackedSubnets()
	for subnetID := range m.trackedSubnets {
		if trackedSubnets.Contains(subnetID) {
			m.numSubnetPeers.WithLabelValues(subnetID.String()).Inc()
		}
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	now := float64(time.Now().UnixNano())
	m.peerConnectedStartTimes[peer.ID()] = now
	m.peerConnectedStartTimesSum += now
}

func (m *metrics) markDisconnected(peer peer.Peer) {
	m.numPeers.Dec()
	m.disconnected.Inc()

	trackedSubnets := peer.TrackedSubnets()
	for subnetID := range m.trackedSubnets {
		if trackedSubnets.Contains(subnetID) {
			m.numSubnetPeers.WithLabelValues(subnetID.String()).Dec()
		}
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	peerID := peer.ID()
	start := m.peerConnectedStartTimes[peerID]
	m.peerConnectedStartTimesSum -= start

	delete(m.peerConnectedStartTimes, peerID)
}

func (m *metrics) updatePeerConnectionLifetimeMetrics() {
	m.lock.RLock()
	defer m.lock.RUnlock()

	avg := float64(0)
	if n := len(m.peerConnectedStartTimes); n > 0 {
		avgStartTime := m.peerConnectedStartTimesSum / float64(n)
		avg = float64(time.Now().UnixNano()) - avgStartTime
	}

	m.peerConnectedLifetimeAverage.Set(avg)
}
