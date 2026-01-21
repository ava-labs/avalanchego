// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// NewCounterHandler returns a TestHandler that wraps the given innerHandler.
// The returned handler will call fn when the AppRequest method has been called
// index times. fn must be thread-safe only if called externally.
func NewCounterHandler(innerHandler p2p.Handler, fn func(), index int) *p2p.TestHandler {
	interceptor := &p2p.TestHandler{}
	count := atomic.Int32{}
	count.Store(-1)
	interceptor.AppRequestF = func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
		if count.Add(1) == int32(index) {
			fn()
		}
		return innerHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
	}
	return interceptor
}
