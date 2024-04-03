// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ MessageQueue = (*messageQueue)(nil)

// Message defines individual messages that have been parsed from the network
// and are now pending execution from the chain.
type Message struct {
	// The original message from the peer
	message.InboundMessage
	// The desired engine type to execute this message. If not specified,
	// the current executing engine type is used.
	EngineType p2p.EngineType
}

type MessageQueue interface {
	// Add a message.
	//
	// If called after [Shutdown], the message will immediately be marked as
	// having been handled.
	Push(context.Context, Message)

	// Remove and return a message and its context.
	//
	// If there are no available messages, this function will block until a
	// message becomes available or the queue is [Shutdown].
	Pop() (context.Context, Message, bool)

	// Returns the number of messages currently on the queue
	Len() int

	// Shutdown and empty the queue.
	Shutdown()
}

// TODO: Use a better data structure for this.
// We can do something better than pushing to the back of a queue. A multi-level
// queue?
type messageQueue struct {
	// Useful for faking time in tests
	clock   mockable.Clock
	metrics messageQueueMetrics

	ctx *snow.ConsensusContext
	// Validator set for the chain associated with this
	vdrs validators.Manager
	// Tracks CPU utilization of each node
	cpuTracker tracker.Tracker

	cond   *sync.Cond
	closed bool
	// Node ID --> Messages this node has in [msgs]
	nodeToUnprocessedMsgs map[ids.NodeID]int
	// Unprocessed messages
	msgAndCtxs buffer.Deque[*msgAndContext]
}

func NewMessageQueue(
	ctx *snow.ConsensusContext,
	vdrs validators.Manager,
	cpuTracker tracker.Tracker,
	metricsNamespace string,
	ops []message.Op,
) (MessageQueue, error) {
	m := &messageQueue{
		ctx:                   ctx,
		vdrs:                  vdrs,
		cpuTracker:            cpuTracker,
		cond:                  sync.NewCond(&sync.Mutex{}),
		nodeToUnprocessedMsgs: make(map[ids.NodeID]int),
		msgAndCtxs:            buffer.NewUnboundedDeque[*msgAndContext](1 /*=initSize*/),
	}
	return m, m.metrics.initialize(metricsNamespace, ctx.Registerer, ops)
}

func (m *messageQueue) Push(ctx context.Context, msg Message) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.closed {
		msg.OnFinishedHandling()
		return
	}

	// Add the message to the queue
	m.msgAndCtxs.PushRight(&msgAndContext{
		msg: msg,
		ctx: ctx,
	})
	m.nodeToUnprocessedMsgs[msg.NodeID()]++

	// Update metrics
	m.metrics.nodesWithMessages.Set(float64(len(m.nodeToUnprocessedMsgs)))
	m.metrics.len.Inc()
	m.metrics.ops[msg.Op()].Inc()

	// Signal a waiting thread
	m.cond.Signal()
}

// FIFO, but skip over messages whose senders whose messages have caused us to
// use excessive CPU recently.
func (m *messageQueue) Pop() (context.Context, Message, bool) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	for {
		if m.closed {
			return nil, Message{}, false
		}
		if m.msgAndCtxs.Len() != 0 {
			break
		}
		m.cond.Wait()
	}

	n := m.msgAndCtxs.Len() // note that n > 0
	i := 0
	for {
		if i == n {
			m.ctx.Log.Debug("canPop is false for all unprocessed messages",
				zap.Int("numMessages", n),
			)
		}

		var (
			msgAndCtx, _ = m.msgAndCtxs.PopLeft()
			msg          = msgAndCtx.msg
			ctx          = msgAndCtx.ctx
			nodeID       = msg.NodeID()
		)

		// See if it's OK to process [msg] next
		if m.canPop(msg) || i == n { // i should never == n but handle anyway as a fail-safe
			m.nodeToUnprocessedMsgs[nodeID]--
			if m.nodeToUnprocessedMsgs[nodeID] == 0 {
				delete(m.nodeToUnprocessedMsgs, nodeID)
			}
			m.metrics.nodesWithMessages.Set(float64(len(m.nodeToUnprocessedMsgs)))
			m.metrics.len.Dec()
			m.metrics.ops[msg.Op()].Dec()
			return ctx, msg, true
		}
		// [msg.nodeID] is causing excessive CPU usage.
		// Push [msg] to back of [m.msgs] and handle it later.
		m.msgAndCtxs.PushRight(msgAndCtx)
		i++
		m.metrics.numExcessiveCPU.Inc()
	}
}

func (m *messageQueue) Len() int {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	return m.msgAndCtxs.Len()
}

func (m *messageQueue) Shutdown() {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	// Remove all the current messages from the queue
	for m.msgAndCtxs.Len() > 0 {
		msgAndCtx, _ := m.msgAndCtxs.PopLeft()
		msgAndCtx.msg.OnFinishedHandling()
	}
	m.nodeToUnprocessedMsgs = nil

	// Update metrics
	m.metrics.nodesWithMessages.Set(0)
	m.metrics.len.Set(0)

	// Mark the queue as closed
	m.closed = true
	m.cond.Broadcast()
}

// canPop will return true for at least one message in [m.msgs]
func (m *messageQueue) canPop(msg message.InboundMessage) bool {
	// Always process messages (avoids iterating over queue for each delivery)
	return true
}

type msgAndContext struct {
	msg Message
	ctx context.Context
}
