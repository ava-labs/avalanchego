// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

var _ InboundMsgThrottler = (*noInboundMsgThrottler)(nil)

// Returns an InboundMsgThrottler where Acquire() always returns immediately.
func NewNoInboundThrottler() InboundMsgThrottler {
	return &noInboundMsgThrottler{}
}

// [Acquire] always returns immediately.
type noInboundMsgThrottler struct{}

func (*noInboundMsgThrottler) Acquire(context.Context, uint64, ids.NodeID) ReleaseFunc {
	return noopRelease
}

func (*noInboundMsgThrottler) AddNode(ids.NodeID) {}

func (*noInboundMsgThrottler) RemoveNode(ids.NodeID) {}
