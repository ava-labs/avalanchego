// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Handler = (*ThrottledHandler)(nil)

type ThrottledHandler struct {
	Handler
	Throttler Throttler
}

func (t ThrottledHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if err := t.Throttler.Throttle(ctx, nodeID, 1); err != nil {
		return nil, err
	}

	return t.Handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}
