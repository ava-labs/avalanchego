// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ MessageQueue = &messageQueue{}

type MessageQueue interface {
	// Add a message.
	//
	// If called after [Shutdown], the message will immediately be marked as
	// having been handled.
	Push(message.InboundMessage)

	// Get and remove a message.
	//
	// If there are no available messages, this function will block until a
	// message becomes available or the queue is [Shutdown].
	Pop() (message.InboundMessage, bool)

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

	log logging.Logger
	// Validator set for the chain associated with this
	vdrs validators.Set
	// Tracks CPU utilization of each node
	cpuTracker tracker.TimeTracker

	cond   *sync.Cond
	closed bool
	// Node ID --> Messages this node has in [msgs]
	nodeToUnprocessedMsgs map[ids.ShortID]int
	// Unprocessed messages
	msgs []message.InboundMessage
}

func NewMessageQueue(
	log logging.Logger,
	vdrs validators.Set,
	cpuTracker tracker.TimeTracker,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) (MessageQueue, error) {
	m := &messageQueue{
		log:                   log,
		vdrs:                  vdrs,
		cpuTracker:            cpuTracker,
		cond:                  sync.NewCond(&sync.Mutex{}),
		nodeToUnprocessedMsgs: make(map[ids.ShortID]int),
	}
	return m, m.metrics.initialize(metricsNamespace, metricsRegisterer)
}

func (m *messageQueue) Push(msg message.InboundMessage) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.closed {
		msg.OnFinishedHandling()
		return
	}

	// Add the message to the queue
	m.msgs = append(m.msgs, msg)
	m.nodeToUnprocessedMsgs[msg.NodeID()]++

	// Update metrics
	m.metrics.nodesWithMessages.Set(float64(len(m.nodeToUnprocessedMsgs)))
	m.metrics.len.Inc()

	// Signal a waiting thread
	m.cond.Signal()
}

// FIFO, but skip over messages whose senders whose messages have caused us to
// use excessive CPU recently.
func (m *messageQueue) Pop() (message.InboundMessage, bool) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	for {
		if m.closed {
			return nil, false
		}
		if len(m.msgs) != 0 {
			break
		}
		m.cond.Wait()
	}

	n := len(m.msgs)
	i := 0
	for {
		if i == n {
			m.log.Debug("canPop is false for all %d unprocessed messages", n)
		}
		msg := m.msgs[0]
		nodeID := msg.NodeID()
		// See if it's OK to process [msg] next
		if m.canPop(msg) || i == n { // i should never == n but handle anyway as a fail-safe
			if cap(m.msgs) == 1 {
				m.msgs = nil // Give back memory if possible
			} else {
				m.msgs = m.msgs[1:]
			}
			m.nodeToUnprocessedMsgs[nodeID]--
			if m.nodeToUnprocessedMsgs[nodeID] == 0 {
				delete(m.nodeToUnprocessedMsgs, nodeID)
			}
			m.metrics.nodesWithMessages.Set(float64(len(m.nodeToUnprocessedMsgs)))
			m.metrics.len.Dec()
			return msg, true
		}
		// [msg.nodeID] is causing excessive CPU usage.
		// Push [msg] to back of [m.msgs] and handle it later.
		m.msgs = append(m.msgs, msg)
		m.msgs = m.msgs[1:]
		i++
		m.metrics.numExcessiveCPU.Inc()
	}
}

func (m *messageQueue) Len() int {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	return len(m.msgs)
}

func (m *messageQueue) Shutdown() {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	// Remove all the current messages from the queue
	for _, msg := range m.msgs {
		msg.OnFinishedHandling()
	}
	m.msgs = nil
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
	// Always pop connected and disconnected messages.
	if op := msg.Op(); op == message.Connected || op == message.Disconnected {
		return true
	}

	// If the deadline to handle [msg] has passed, always pop it.
	// It will be dropped immediately.
	if expirationTime := msg.ExpirationTime(); !expirationTime.IsZero() && m.clock.Time().After(expirationTime) {
		return true
	}
	// Every node has some allowed CPU allocation depending on
	// the number of nodes with unprocessed messages.
	baseMaxCPU := 1 / float64(len(m.nodeToUnprocessedMsgs))
	nodeID := msg.NodeID()
	weight, isVdr := m.vdrs.GetWeight(nodeID)
	if !isVdr {
		weight = 0
	}
	// The sum of validator weights should never be 0, but handle
	// that case for completeness here to avoid divide by 0.
	portionWeight := float64(0)
	totalVdrsWeight := m.vdrs.Weight()
	if totalVdrsWeight != 0 {
		portionWeight = float64(weight) / float64(totalVdrsWeight)
	}
	// Validators are allowed to use more CPm. More weight --> more CPU use allowed.
	recentCPUUtilized := m.cpuTracker.Utilization(nodeID, m.clock.Time())
	maxCPU := baseMaxCPU + (1.0-baseMaxCPU)*portionWeight
	return recentCPUUtilized <= maxCPU
}
