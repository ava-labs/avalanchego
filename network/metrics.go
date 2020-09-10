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
	numSent, numFailed, numReceived prometheus.Counter
}

func (mm *messageMetrics) initialize(msgType Op, registerer prometheus.Registerer) error {
	mm.numSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: constants.PlatformName,
			Name:      fmt.Sprintf("%s_sent", msgType),
			Help:      fmt.Sprintf("Number of %s messages sent", msgType),
		})
	mm.numFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: constants.PlatformName,
			Name:      fmt.Sprintf("%s_failed", msgType),
			Help:      fmt.Sprintf("Number of %s messages that failed to be sent", msgType),
		})
	mm.numReceived = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: constants.PlatformName,
			Name:      fmt.Sprintf("%s_received", msgType),
			Help:      fmt.Sprintf("Number of %s messages received", msgType),
		})

	if err := registerer.Register(mm.numSent); err != nil {
		return fmt.Errorf("failed to register sent statistics of %s due to %s",
			msgType, err)
	}
	if err := registerer.Register(mm.numFailed); err != nil {
		return fmt.Errorf("failed to register failed statistics of %s due to %s",
			msgType, err)
	}
	if err := registerer.Register(mm.numReceived); err != nil {
		return fmt.Errorf("failed to register received statistics of %s due to %s",
			msgType, err)
	}
	return nil
}

type metrics struct {
	numPeers prometheus.Gauge

	getVersion, version,
	getPeerlist, peerlist,
	ping, pong,
	getAcceptedFrontier, acceptedFrontier,
	getAccepted, accepted,
	get, getAncestors, put, multiPut,
	pushQuery, pullQuery, chits messageMetrics
}

func (m *metrics) initialize(registerer prometheus.Registerer) error {
	m.numPeers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: constants.PlatformName,
			Name:      "peers",
			Help:      "Number of network peers",
		})

	errs := wrappers.Errs{}
	if err := registerer.Register(m.numPeers); err != nil {
		errs.Add(fmt.Errorf("failed to register peers statistics due to %s",
			err))
	}

	errs.Add(m.getVersion.initialize(GetVersion, registerer))
	errs.Add(m.version.initialize(Version, registerer))
	errs.Add(m.getPeerlist.initialize(GetPeerList, registerer))
	errs.Add(m.peerlist.initialize(PeerList, registerer))
	errs.Add(m.ping.initialize(Ping, registerer))
	errs.Add(m.pong.initialize(Pong, registerer))
	errs.Add(m.getAcceptedFrontier.initialize(GetAcceptedFrontier, registerer))
	errs.Add(m.acceptedFrontier.initialize(AcceptedFrontier, registerer))
	errs.Add(m.getAccepted.initialize(GetAccepted, registerer))
	errs.Add(m.accepted.initialize(Accepted, registerer))
	errs.Add(m.get.initialize(Get, registerer))
	errs.Add(m.getAncestors.initialize(GetAncestors, registerer))
	errs.Add(m.put.initialize(Put, registerer))
	errs.Add(m.multiPut.initialize(MultiPut, registerer))
	errs.Add(m.pushQuery.initialize(PushQuery, registerer))
	errs.Add(m.pullQuery.initialize(PullQuery, registerer))
	errs.Add(m.chits.initialize(Chits, registerer))

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
		return &m.peerlist
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
	case Get:
		return &m.get
	case GetAncestors:
		return &m.getAncestors
	case Put:
		return &m.put
	case MultiPut:
		return &m.multiPut
	case PushQuery:
		return &m.pushQuery
	case PullQuery:
		return &m.pullQuery
	case Chits:
		return &m.chits
	default:
		return nil
	}
}
