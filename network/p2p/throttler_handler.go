// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

type ThrottlerHandler struct {
	Handler
	Throttler Throttler
	Log       logging.Logger
}

func (t ThrottlerHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !t.Throttler.Handle(nodeID) {
		t.Log.Debug("dropping message", zap.Stringer("nodeID", nodeID))
		return
	}

	t.Handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t ThrottlerHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if !t.Throttler.Handle(nodeID) {
		return nil, fmt.Errorf("dropping message from %s: %w", nodeID, ErrThrottled)
	}

	return t.Handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}
