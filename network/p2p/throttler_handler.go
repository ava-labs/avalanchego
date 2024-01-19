// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	ErrThrottled         = errors.New("throttled")
	_            Handler = (*ThrottlerHandler)(nil)
)

func NewThrottlerHandler(handler Handler, throttler Throttler, log logging.Logger) *ThrottlerHandler {
	return &ThrottlerHandler{
		handler:   handler,
		throttler: throttler,
		log:       log,
	}
}

type ThrottlerHandler struct {
	handler   Handler
	throttler Throttler
	log       logging.Logger
}

func (t ThrottlerHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !t.throttler.Handle(nodeID) {
		t.log.Debug(
			"dropping message",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "throttled"),
		)
		return
	}

	t.handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t ThrottlerHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if !t.throttler.Handle(nodeID) {
		return nil, fmt.Errorf("dropping message from %s: %w", nodeID, ErrThrottled)
	}

	return t.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

func (t ThrottlerHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	return t.handler.CrossChainAppRequest(ctx, chainID, deadline, requestBytes)
}
