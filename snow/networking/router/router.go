// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/avalanchego/health"
	"github.com/ava-labs/avalanchego/ids"
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
	RegisterRequest(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		msgType constants.MsgType,
	)
	GetAcceptedFrontier(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		onFinishedHandling func(),
	)
	AcceptedFrontier(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		onFinishedHandling func(),
	)
	GetAccepted(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		containerIDs []ids.ID,
		onFinishedHandling func(),
	)
	Accepted(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		onFinishedHandling func(),
	)
	GetAncestors(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		containerID ids.ID,
		onFinishedHandling func(),
	)
	MultiPut(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
		onFinishedHandling func(),
	)
	Get(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		containerID ids.ID,
		onFinishedHandling func(),
	)
	Put(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
		onFinishedHandling func(),
	)
	PushQuery(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		containerID ids.ID,
		container []byte,
		onFinishedHandling func(),
	)
	PullQuery(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		containerID ids.ID,
		onFinishedHandling func(),
	)
	Chits(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		votes []ids.ID,
		onFinishedHandling func(),
	)
	AppRequest(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		appRequestBytes []byte,
		onFinishedHandling func(),
	)
	AppResponse(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		appResponseBytes []byte,
		onFinishedHandling func(),
	)
	AppGossip(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		appGossipBytes []byte,
		onFinishedHandling func(),
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
	Connected(nodeID ids.ShortID)
	Disconnected(nodeID ids.ShortID)
	AppRequestFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32)
}
