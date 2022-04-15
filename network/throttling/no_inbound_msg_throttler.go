// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import "github.com/chain4travel/caminogo/ids"

var _ InboundMsgThrottler = &noInboundMsgThrottler{}

// Returns an InboundMsgThrottler where Acquire() always returns immediately.
func NewNoInboundThrottler() InboundMsgThrottler {
	return &noInboundMsgThrottler{}
}

// [Acquire] always returns immediately.
type noInboundMsgThrottler struct{}

func (*noInboundMsgThrottler) Acquire(uint64, ids.ShortID) {}

func (*noInboundMsgThrottler) Release(uint64, ids.ShortID) {}

func (*noInboundMsgThrottler) AddNode(ids.ShortID) {}

func (*noInboundMsgThrottler) RemoveNode(ids.ShortID) {}
