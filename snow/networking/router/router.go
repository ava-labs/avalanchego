// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/avalanchego/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
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
		msgCreator message.Creator,
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
	HandleInbound(msg message.InboundMessage)

	RegisterRequest(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		op message.Op,
	)
}

// InternalRouter deals with messages internal to this node
type InternalRouter interface {
	benchlist.Benchable

	Connected(nodeID ids.ShortID)
	Disconnected(nodeID ids.ShortID)
}
