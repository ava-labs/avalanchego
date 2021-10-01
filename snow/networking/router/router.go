// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/avalanchego/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

// Router routes consensus messages to the Handler of the consensus
// engine that the messages are intended for
type Router interface {
	ExternalRouter
	InternalRouter

	Initialize(
		nodeID ids.ShortID,
		log logging.Logger,
		timeouts *timeout.Manager,
		gossipFrequency,
		shutdownTimeout time.Duration,
		criticalChains ids.Set,
		onFatal func(exitCode int),
		healthConfig HealthConfig,
		metricsNamespace string,
		metricsRegisterer prometheus.Registerer,
	) error
	Shutdown()
	AddChain(chain *Handler)
	health.Checkable
}

// ExternalRouter routes messages from the network to the
// Handler of the consensus engine that the message is intended for
type ExternalRouter interface {
	HandleInbound(
		msgType constants.MsgType,
		msg message.InboundMessage,
		nodeID ids.ShortID,
		onFinishedHandling func(),
	)

	RegisterRequest(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		msgType constants.MsgType,
	)
}

// InternalRouter deals with messages internal to this node
type InternalRouter interface {
	benchlist.Benchable

	GetAcceptedFrontierFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32)
	GetAcceptedFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32)
	GetFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32)
	GetAncestorsFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32)
	QueryFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32)

	AppRequestFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32)

	Connected(nodeID ids.ShortID)
	Disconnected(nodeID ids.ShortID)
}
