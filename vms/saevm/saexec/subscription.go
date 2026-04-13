// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
)

func (e *Executor) sendPostExecutionEvents(b *types.Block, receipts types.Receipts) {
	e.headEvents.Send(core.ChainHeadEvent{Block: b})

	var logs []*types.Log
	for _, r := range receipts {
		logs = append(logs, r.Logs...)
	}
	e.chainEvents.Send(core.ChainEvent{
		Block: b,
		Hash:  b.Hash(),
		Logs:  logs,
	})
	e.logEvents.Send(logs)
}

// SubscribeChainHeadEvent returns a new subscription for each
// [core.ChainHeadEvent] emitted after execution of a [blocks.Block].
func (e *Executor) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return e.headEvents.Subscribe(ch)
}

// SubscribeChainEvent returns a new subscription for each [core.ChainEvent]
// emitted after execution of a [blocks.Block].
func (e *Executor) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return e.chainEvents.Subscribe(ch)
}

// SubscribeLogsEvent returns a new subscription for logs emitted after
// execution of a [blocks.Block].
func (e *Executor) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return e.logEvents.Subscribe(ch)
}
