// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ MessageQueue = (*messageQueue)(nil)

// Message defines individual messages that have been parsed from the network
// and are now pending execution from the chain.
type Message struct {
	// The original message from the peer
	*message.InboundMessage

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

	log      logging.Logger
	subnetID ids.ID
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
	log logging.Logger,
	subnetID ids.ID,
	vdrs validators.Manager,
	cpuTracker tracker.Tracker,
	metricsNamespace string,
	reg prometheus.Registerer,
) (MessageQueue, error) {
	m := &messageQueue{
		log:                   log,
		subnetID:              subnetID,
		vdrs:                  vdrs,
		cpuTracker:            cpuTracker,
		cond:                  sync.NewCond(&sync.Mutex{}),
		nodeToUnprocessedMsgs: make(map[ids.NodeID]int),
		msgAndCtxs:            buffer.NewUnboundedDeque[*msgAndContext](1 /*=initSize*/),
	}
	return m, m.metrics.initialize(metricsNamespace, reg)
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
	m.nodeToUnprocessedMsgs[msg.NodeID]++

	// Update metrics
	m.metrics.count.With(prometheus.Labels{
		opLabel: msg.Op.String(),
	}).Inc()
	m.metrics.nodesWithMessages.Set(float64(len(m.nodeToUnprocessedMsgs)))

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
			m.log.Debug("canPop is false for all unprocessed messages",
				zap.Int("numMessages", n),
			)
		}

		var (
			msgAndCtx, _ = m.msgAndCtxs.PopLeft()
			msg          = msgAndCtx.msg
			ctx          = msgAndCtx.ctx
			nodeID       = msg.NodeID
		)

		// See if it's OK to process [msg] next
		if m.canPop(msg.InboundMessage) || i == n { // i should never == n but handle anyway as a fail-safe
			m.nodeToUnprocessedMsgs[nodeID]--
			if m.nodeToUnprocessedMsgs[nodeID] == 0 {
				delete(m.nodeToUnprocessedMsgs, nodeID)
			}
			m.metrics.count.With(prometheus.Labels{
				opLabel: msg.Op.String(),
			}).Dec()
			m.metrics.nodesWithMessages.Set(float64(len(m.nodeToUnprocessedMsgs)))
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
	m.metrics.count.Reset()
	m.metrics.nodesWithMessages.Set(0)

	// Mark the queue as closed
	m.closed = true
	m.cond.Broadcast()
}

// canPop will return true for at least one message in [m.msgs]
func (m *messageQueue) canPop(msg *message.InboundMessage) bool {
	// Always pop connected and disconnected messages.
	if op := msg.Op; op == message.ConnectedOp || op == message.DisconnectedOp {
		return true
	}

	// If the deadline to handle [msg] has passed, always pop it.
	// It will be dropped immediately.
	if expiration := msg.Expiration; m.clock.Time().After(expiration) {
		return true
	}
	// Every node has some allowed CPU allocation depending on
	// the number of nodes with unprocessed messages.
	baseMaxCPU := 1 / float64(len(m.nodeToUnprocessedMsgs))
	nodeID := msg.NodeID
	weight := m.vdrs.GetWeight(m.subnetID, nodeID)

	var portionWeight float64
	if totalVdrsWeight, err := m.vdrs.TotalWeight(m.subnetID); err != nil {
		// The sum of validator weights should never overflow, but if they do,
		// we treat portionWeight as 0.
		m.log.Error("failed to get total weight of validators",
			zap.Stringer("subnetID", m.subnetID),
			zap.Error(err),
		)
	} else if totalVdrsWeight == 0 {
		// The sum of validator weights should never be 0, but handle that case
		// for completeness here to avoid divide by 0.
		m.log.Warn("validator set is empty",
			zap.Stringer("subnetID", m.subnetID),
		)
	} else {
		portionWeight = float64(weight) / float64(totalVdrsWeight)
	}

	// Validators are allowed to use more CPU. More weight --> more CPU use allowed.
	recentCPUUsage := m.cpuTracker.Usage(nodeID, m.clock.Time())
	maxCPU := baseMaxCPU + (1.0-baseMaxCPU)*portionWeight
	return recentCPUUsage <= maxCPU
}

type msgAndContext struct {
	msg Message
	ctx context.Context
}
