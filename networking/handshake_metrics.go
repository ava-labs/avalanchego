// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/utils/logging"
)

type handshakeMetrics struct {
	numPeers prometheus.Gauge

	numGetVersionSent, numGetVersionReceived,
	numVersionSent, numVersionReceived,
	numGetPeerlistSent, numGetPeerlistReceived,
	numPeerlistSent, numPeerlistReceived prometheus.Counter
}

func (hm *handshakeMetrics) Initialize(log logging.Logger, registerer prometheus.Registerer) {
	hm.numPeers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "gecko",
			Name:      "peers",
			Help:      "Number of network peers",
		})
	hm.numGetVersionSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_version_sent",
			Help:      "Number of get_version messages sent",
		})
	hm.numGetVersionReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_version_received",
			Help:      "Number of get_version messages received",
		})
	hm.numVersionSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "version_sent",
			Help:      "Number of version messages sent",
		})
	hm.numVersionReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "version_received",
			Help:      "Number of version messages received",
		})
	hm.numGetPeerlistSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_peerlist_sent",
			Help:      "Number of get_peerlist messages sent",
		})
	hm.numGetPeerlistReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "get_peerlist_received",
			Help:      "Number of get_peerlist messages received",
		})
	hm.numPeerlistSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "peerlist_sent",
			Help:      "Number of peerlist messages sent",
		})
	hm.numPeerlistReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "gecko",
			Name:      "peerlist_received",
			Help:      "Number of peerlist messages received",
		})

	if err := registerer.Register(hm.numPeers); err != nil {
		log.Error("Failed to register peers statistics due to %s", err)
	}
	if err := registerer.Register(hm.numGetVersionSent); err != nil {
		log.Error("Failed to register get_version_sent statistics due to %s", err)
	}
	if err := registerer.Register(hm.numGetVersionReceived); err != nil {
		log.Error("Failed to register get_version_received statistics due to %s", err)
	}
	if err := registerer.Register(hm.numVersionSent); err != nil {
		log.Error("Failed to register version_sent statistics due to %s", err)
	}
	if err := registerer.Register(hm.numVersionReceived); err != nil {
		log.Error("Failed to register version_received statistics due to %s", err)
	}
	if err := registerer.Register(hm.numGetPeerlistSent); err != nil {
		log.Error("Failed to register get_peerlist_sent statistics due to %s", err)
	}
	if err := registerer.Register(hm.numGetPeerlistReceived); err != nil {
		log.Error("Failed to register get_peerlist_received statistics due to %s", err)
	}
	if err := registerer.Register(hm.numPeerlistSent); err != nil {
		log.Error("Failed to register peerlist_sent statistics due to %s", err)
	}
	if err := registerer.Register(hm.numPeerlistReceived); err != nil {
		log.Error("Failed to register peerlist_received statistics due to %s", err)
	}
}
