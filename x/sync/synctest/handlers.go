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

func AddFuncOnIntercept(interceptor *p2p.TestHandler, innerHandler p2p.Handler, fn func(), numToAllow int) {
	count := atomic.Int32{}
	count.Store(-1)
	interceptor.AppRequestF = func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
		if count.Add(1) == int32(numToAllow) {
			fn()
		}
		return innerHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
	}
}
