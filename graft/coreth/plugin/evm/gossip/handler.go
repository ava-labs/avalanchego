// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ p2p.Handler = (*txGossipHandler)(nil)

func NewTxGossipHandler[T gossip.Gossipable](
	log logging.Logger,
	marshaller gossip.Marshaller[T],
	mempool gossip.Set[T],
	metrics gossip.Metrics,
	maxMessageSize int,
	throttlingPeriod time.Duration,
	throttlingLimit int,
	validators p2p.ValidatorSet,
) *txGossipHandler {
	// push gossip messages can be handled from any peer
	handler := gossip.NewHandler(
		log,
		marshaller,
		mempool,
		metrics,
		maxMessageSize,
	)

	// pull gossip requests are filtered by validators and are throttled
	// to prevent spamming
	validatorHandler := p2p.NewValidatorHandler(
		p2p.NewThrottlerHandler(
			handler,
			p2p.NewSlidingWindowThrottler(throttlingPeriod, throttlingLimit),
			log,
		),
		validators,
		log,
	)

	return &txGossipHandler{
		appGossipHandler:  handler,
		appRequestHandler: validatorHandler,
	}
}

type txGossipHandler struct {
	appGossipHandler  p2p.Handler
	appRequestHandler p2p.Handler
}

func (t *txGossipHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	t.appGossipHandler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t *txGossipHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	return t.appRequestHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
}
