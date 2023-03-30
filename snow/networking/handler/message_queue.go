// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"math"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

const (
	// If a node's CPU utilization ratio is off by less than [allowedTargetDelta],
	// it stays within the same utilization bucket.
	allowedTargetDelta = 0.05

	// This is just a hint and can be any value.
	initialMessageQueueSize = 32
)

var (
	_ MessageQueue = &multilevelMessageQueue{}

	// Determines the probability with which we pop from a given bucket first.
	// TODO are these values appropriate?
	// TODO make this configurable
	bucketWeights = []uint64{8, 7, 6, 5, 4}
)

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

	// Get and remove a message.
	// Returns false if the MessageQueue is closed.
	//
	// If there are no available messages, this function will block until a
	// message becomes available or the queue is [Shutdown].
	Pop() (context.Context, Message, bool)

	// Returns the number of messages currently on the queue
	Len() int

	// Shutdown and empty the queue.
	Shutdown()
}

// The queue of messages waiting to be handled from a node.
// Not safe for concurrent use.
type nodeMessageQueue struct {
	// Messages from this node waiting to be handled
	messages buffer.Deque[*msgAndContext]

	// The node that this queue is associated with
	nodeID ids.NodeID

	// The bucket this node belongs to
	bucket *utilizationBucket
}

func newNodeMessageQueue(nodeID ids.NodeID) *nodeMessageQueue {
	return &nodeMessageQueue{
		messages: buffer.NewUnboundedDeque[*msgAndContext](initialMessageQueueSize),
		nodeID:   nodeID,
	}
}

func (n *nodeMessageQueue) push(ctx context.Context, msg Message) {
	n.messages.PushRight(&msgAndContext{
		msg: msg,
		ctx: ctx,
	})
}

// Returns the next message in the queue.
// Returns false iff there are no more messages after this one.
// Invariant: There is at least 1 message in the queue.
func (n *nodeMessageQueue) pop() (context.Context, Message, bool) {
	msgAndCtx, _ := n.messages.PopLeft()
	return msgAndCtx.ctx, msgAndCtx.msg, n.messages.Len() > 0
}

// A group of node message queues that share a priority.
// Within this bucket, each node's message queue is read from in turn.
// Not safe for concurrent use.
type utilizationBucket struct {
	// Invariant: each queue in this list has >= 1 message.
	nodeQueues []*nodeMessageQueue
	// Node ID --> Index of the node in [nodeQueues]
	nodeToIndex map[ids.NodeID]int
	// the index of the node to read from next. Changes after each read.
	// Invariant: Always in [0, len(nodeQueues)-1]
	currentNodeIndex int
	// Index of this bucket within a list of buckets.
	index int
}

func newUtilizationBucket(index int) *utilizationBucket {
	return &utilizationBucket{
		index:       index,
		nodeQueues:  make([]*nodeMessageQueue, 0),
		nodeToIndex: make(map[ids.NodeID]int),
	}
}

// Assumes [queue] has >= 1 message to honor [u.nodeQueues] invariant.
func (u *utilizationBucket) addNodeMessageQueue(queue *nodeMessageQueue) {
	u.nodeQueues = append(u.nodeQueues, queue)
	queue.bucket = u
	u.nodeToIndex[queue.nodeID] = len(u.nodeQueues) - 1
}

// Assumes [nodeID]'s queue is in this bucket.
func (u *utilizationBucket) removeNodeMessageQueue(nodeID ids.NodeID) {
	currentIndex := u.nodeToIndex[nodeID]
	lastIndex := len(u.nodeQueues) - 1 // note [lastIndex] >= 0

	// Swap the last node into the current node's slot, then shrink the list
	u.nodeQueues[currentIndex] = u.nodeQueues[lastIndex]
	u.nodeToIndex[u.nodeQueues[lastIndex].nodeID] = currentIndex
	u.nodeQueues[lastIndex] = nil
	u.nodeQueues = u.nodeQueues[:lastIndex]

	delete(u.nodeToIndex, nodeID)

	// Reset [currentNodeIndex] if it's now out of bounds.
	if u.currentNodeIndex >= len(u.nodeQueues) {
		u.currentNodeIndex = 0
	}
}

// Returns the message queue we should read from next.
func (u *utilizationBucket) getNextQueue() *nodeMessageQueue {
	queue := u.nodeQueues[u.currentNodeIndex]

	// Next time we get a message from this bucket, get it from the next node.
	// Wrap around if we're at the end of the list.
	u.currentNodeIndex++
	u.currentNodeIndex %= len(u.nodeQueues)
	return queue
}

// Splits nodes' queues into buckets based on node cpu utilization.
// Spends less time on messages from buckets with higher utilization.
// A node's bucket is updated when messages are pushed or popped from that node's queue.
type multilevelMessageQueue struct {
	// Useful for faking time in tests
	clock   mockable.Clock
	metrics *messageQueueMetrics

	log logging.Logger

	// Tracks CPU utilization of each node
	cpuTracker tracker.Tracker

	// Calculates CPU target for each node
	cpuTargeter tracker.Targeter

	cond *sync.Cond

	// We sample from this to determine which bucket to
	// try to pop from first.
	sampler sampler.WeightedWithoutReplacement

	closed bool
	// Node ID --> Queue of messages from that node
	nodeMap map[ids.NodeID]*nodeMessageQueue
	// Highest --> Lowest priority buckets.
	// Length must be > 0.
	utilizatonBuckets []*utilizationBucket
	// Total number of messages waiting to be handled.
	numProcessing int
}

func NewMessageQueue(
	log logging.Logger,
	cpuTracker tracker.Tracker,
	cpuTargeter tracker.Targeter,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
	ops []message.Op,
) (MessageQueue, error) {
	sampler := sampler.NewBestWeightedWithoutReplacement(1)
	if err := sampler.Initialize(bucketWeights); err != nil {
		return nil, err
	}

	metrics, err := newMessageQueueMetrics(metricsNamespace, metricsRegisterer, ops)
	if err != nil {
		return nil, err
	}
	m := &multilevelMessageQueue{
		log:               log,
		metrics:           metrics,
		cpuTracker:        cpuTracker,
		cpuTargeter:       cpuTargeter,
		cond:              sync.NewCond(&sync.Mutex{}),
		nodeMap:           make(map[ids.NodeID]*nodeMessageQueue),
		utilizatonBuckets: make([]*utilizationBucket, len(bucketWeights)),
		sampler:           sampler,
	}
	for level := 0; level < len(bucketWeights); level++ {
		m.utilizatonBuckets[level] = newUtilizationBucket(level)
	}

	return m, nil
}

func (m *multilevelMessageQueue) Push(ctx context.Context, msg Message) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.closed {
		msg.OnFinishedHandling()
		return
	}

	var (
		nodeQueue *nodeMessageQueue
		ok        bool
		nodeID    = msg.NodeID()
	)

	// Get this node's message queue.
	// If one doesn't exist, create it and assign it to highest priority bucket.
	if nodeQueue, ok = m.nodeMap[nodeID]; !ok {
		nodeQueue = newNodeMessageQueue(nodeID)
		m.utilizatonBuckets[0].addNodeMessageQueue(nodeQueue)
		m.nodeMap[nodeID] = nodeQueue
	}

	// Add the message to the queue
	nodeQueue.push(ctx, msg)
	m.numProcessing++

	// Ensure this node's queue is in the correct bucket based on utilization
	m.updateNodePriority(nodeQueue)

	// Update metrics
	m.metrics.nodesWithMessages.Set(float64(len(m.nodeMap)))
	m.metrics.len.Inc()
	if opMetric, ok := m.metrics.ops[msg.Op()]; ok {
		opMetric.Inc()
	} else {
		// This should never happen.
		m.log.Warn("unknown message op", zap.Stringer("op", msg.Op()))
	}

	// Signal that there is a new message to handle.
	m.cond.Signal()
}

// the multilevelMessageQueue cycles through the different utilization buckets,
// pulling more messages from lower utilization buckets
func (m *multilevelMessageQueue) Pop() (context.Context, Message, bool) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	for {
		if m.closed {
			return context.Background(), Message{}, false
		}
		if m.numProcessing != 0 {
			break
		}

		// there aren't any messages ready so wait for new messages
		m.cond.Wait()
	}

	// Cycle over the buckets until one with available messages is found.
	// The above for loop guarantees there is at least one queue with messages,
	// and therefore that the loop below will always terminate.
	var queue *nodeMessageQueue
	// Randomly sample the bucket weights to determine which queue to pop from next.
	tryFirst, _ := m.sampler.Sample(1)
	popFromIndex := tryFirst[0]
	for queue == nil {
		if len(m.utilizatonBuckets[popFromIndex].nodeQueues) > 0 {
			queue = m.utilizatonBuckets[popFromIndex].getNextQueue()
			break
		}

		// There were no messages in this bucket. Try the next one.
		// We do this rather than re-sample to avoid the case where we
		// repeatedly sample and inspect empty buckets.
		// Note that we now move to a higher priority bucket and not a lower one
		// (unless we're already at the highest priority bucket).
		popFromIndex--
		if popFromIndex < 0 {
			// Wrap around to lowest priority bucket.
			popFromIndex = len(m.utilizatonBuckets) - 1
		}
	}

	// Note that [queue] is guaranteed to have at least 1 element.
	ctx, msg, hasMoreMessages := queue.pop()
	m.numProcessing--

	// If there are more messages in the queue, update the node's utilization bucket.
	// Otherwise remove the node's queue from consideration.
	if hasMoreMessages {
		m.updateNodePriority(queue)
	} else {
		delete(m.nodeMap, queue.nodeID)
		queue.bucket.removeNodeMessageQueue(queue.nodeID)
	}

	// update metrics
	m.metrics.nodesWithMessages.Set(float64(len(m.nodeMap)))
	m.metrics.len.Dec()
	if opMetric, ok := m.metrics.ops[msg.Op()]; ok {
		opMetric.Dec()
	} else {
		// This should never happen.
		m.log.Warn("unknown message op", zap.Stringer("op", msg.Op()))
	}
	return ctx, msg, true
}

// Update the node's bucket based on its recent CPU utilization.
// Assumes [queue] has >= 1 messages.
func (m *multilevelMessageQueue) updateNodePriority(queue *nodeMessageQueue) {
	var (
		newBucketIndex   = queue.bucket.index
		nodeID           = queue.nodeID
		utilization      = m.cpuTracker.Usage(nodeID, m.clock.Time())
		targetUsage      = m.cpuTargeter.TargetUsage(nodeID)
		utilizationRatio float64
	)

	if targetUsage > 0 {
		utilizationRatio = utilization / targetUsage
	} else {
		// If the target usage is 0 then this node is considered to be
		// using too much CPU.
		utilizationRatio = math.MaxFloat64
	}

	// shift the node's bucket based on the ratio of current utilization vs target utilization
	if utilizationRatio > 1+allowedTargetDelta {
		// Utilization is too high. Deprioritize this bucket.
		// Ensure that the new bucket isn't lower priority than the lowest priority bucket.
		if newBucketIndex < len(m.utilizatonBuckets)-1 {
			newBucketIndex++
		}
	} else if utilizationRatio < 1-allowedTargetDelta {
		// Utilization is too low. Prioritize this bucket.
		// Ensure that the new bucket isn't higher priority than the highest priority bucket.
		if newBucketIndex > 0 {
			newBucketIndex--
		}
	}

	if queue.bucket.index != newBucketIndex {
		queue.bucket.removeNodeMessageQueue(nodeID)
		m.utilizatonBuckets[newBucketIndex].addNodeMessageQueue(queue)
	}
}

func (m *multilevelMessageQueue) Len() int {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	return m.numProcessing
}

func (m *multilevelMessageQueue) Shutdown() {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	// Remove all the current messages from the queue
	for nodeID, nodeQueue := range m.nodeMap {
		for msgAndCtx, ok := nodeQueue.messages.PopLeft(); ok; msgAndCtx, ok = nodeQueue.messages.PopLeft() {
			msgAndCtx.msg.OnFinishedHandling()
		}
		m.nodeMap[nodeID] = nil
	}

	// Clean up memory
	for utilizationLevel := range m.utilizatonBuckets {
		m.utilizatonBuckets[utilizationLevel] = nil
	}

	// Update metrics
	m.metrics.nodesWithMessages.Set(0)
	m.metrics.len.Set(0)

	// Mark the queue as closed
	m.closed = true
	m.cond.Broadcast()
}

type msgAndContext struct {
	msg Message
	ctx context.Context
}
