// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package handler

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Test that when there's only messages from a single node,
// they're handled FIFO.
func TestQueue_FIFO(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cpuTracker := tracker.NewMockTracker(ctrl)
	cpuTracker.EXPECT().Usage(gomock.Any(), gomock.Any()).Return(float64(1)).AnyTimes()

	vdr1ID := ids.GenerateTestNodeID()
	targeter := tracker.NewMockTargeter(ctrl)
	targeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(1)).AnyTimes()
	q, err := NewMessageQueue(logging.NoLog{}, cpuTracker, targeter, "", prometheus.NewRegistry(), message.SynchronousOps)
	require.NoError(err)

	innerMsg1 := message.NewMockInboundMessage(ctrl)
	innerMsg1.EXPECT().NodeID().Return(vdr1ID).AnyTimes()
	innerMsg1.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg1 := Message{
		InboundMessage: innerMsg1,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}
	innerMsg2 := message.NewMockInboundMessage(ctrl)
	innerMsg2.EXPECT().NodeID().Return(vdr1ID).AnyTimes()
	innerMsg2.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg2 := Message{
		InboundMessage: innerMsg2,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}
	innerMsg3 := message.NewMockInboundMessage(ctrl)
	innerMsg3.EXPECT().NodeID().Return(vdr1ID).AnyTimes()
	innerMsg3.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg3 := Message{
		InboundMessage: innerMsg3,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}

	ctx1, ctx2, ctx3 := context.Background(), context.Background(), context.Background()

	q.Push(ctx1, msg1)
	require.EqualValues(1, q.Len())

	q.Push(ctx2, msg2)
	require.EqualValues(2, q.Len())

	q.Push(ctx3, msg3)
	require.EqualValues(3, q.Len())

	// All messages from a single node so it should be FIFO
	ctx, gotMsg1, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx1, ctx)
	require.EqualValues(msg1, gotMsg1)
	require.EqualValues(2, q.Len())

	ctx, gotMsg2, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx2, ctx)
	require.EqualValues(msg2, gotMsg2)
	require.EqualValues(1, q.Len())

	ctx, gotMsg3, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx3, ctx)
	require.EqualValues(msg3, gotMsg3)
	require.EqualValues(0, q.Len())
}

func TestQueue_Multiple_Buckets(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// There are 2 nodes.
	vdr1ID, vdr2ID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

	// Both nodes have target usage of 1.
	targeter := tracker.NewMockTargeter(ctrl)
	targeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(1)).AnyTimes()

	// [vdr1ID] has a usage above the target usage.
	// [vdr2ID] has a usage below the target usage.
	cpuTracker := tracker.NewMockTracker(ctrl)
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(float64(2)).AnyTimes()
	cpuTracker.EXPECT().Usage(vdr2ID, gomock.Any()).Return(float64(0)).AnyTimes()
	q, err := NewMessageQueue(logging.NoLog{}, cpuTracker, targeter, "", prometheus.NewRegistry(), message.SynchronousOps)
	require.NoError(err)

	// Add one message from each node.
	innerMsg1 := message.NewMockInboundMessage(ctrl)
	innerMsg1.EXPECT().NodeID().Return(vdr1ID).AnyTimes()
	innerMsg1.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg1 := Message{
		InboundMessage: innerMsg1,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}
	innerMsg2 := message.NewMockInboundMessage(ctrl)
	innerMsg2.EXPECT().NodeID().Return(vdr2ID).AnyTimes()
	innerMsg2.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg2 := Message{
		InboundMessage: innerMsg2,
		EngineType:     p2p.EngineType_ENGINE_TYPE_AVALANCHE,
	}
	msgs := []message.InboundMessage{msg1, msg2}

	ctx1, ctx2 := context.Background(), context.Background()

	q.Push(ctx1, msg1)
	q.Push(ctx2, msg2)

	// Make sure we get both messages.
	// Note that we're not guaranteed to get them in the order we pushed them.
	ctx, gotMsg1, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx1, ctx)
	require.Contains(msgs, gotMsg1)
	require.Equal(1, q.Len())

	ctx, gotMsg2, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx2, ctx)
	require.Contains(msgs, gotMsg2)
	require.Equal(0, q.Len())

	require.NotEqual(gotMsg1, gotMsg2)
}

func TestQueue_RoundRobin(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// There are 2 nodes with the same target usage and weight.
	vdr1ID, vdr2ID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	cpuTracker := tracker.NewMockTracker(ctrl)
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(float64(0)).AnyTimes()
	cpuTracker.EXPECT().Usage(vdr2ID, gomock.Any()).Return(float64(0)).AnyTimes()

	cpuTargeter := tracker.NewMockTargeter(ctrl)
	cpuTargeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(1)).AnyTimes()

	q, err := NewMessageQueue(logging.NoLog{}, cpuTracker, cpuTargeter, "", prometheus.NewRegistry(), message.SynchronousOps)
	require.NoError(err)

	innerMsg1 := message.NewMockInboundMessage(ctrl)
	innerMsg1.EXPECT().NodeID().Return(vdr1ID)
	innerMsg1.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg1 := Message{
		InboundMessage: innerMsg1,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}
	innerMsg2 := message.NewMockInboundMessage(ctrl)
	innerMsg2.EXPECT().NodeID().Return(vdr1ID)
	innerMsg2.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg2 := Message{
		InboundMessage: innerMsg2,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}
	innerMsg3 := message.NewMockInboundMessage(ctrl)
	innerMsg3.EXPECT().NodeID().Return(vdr2ID)
	innerMsg3.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg3 := Message{
		InboundMessage: innerMsg3,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}
	innerMsg4 := message.NewMockInboundMessage(ctrl)
	innerMsg4.EXPECT().NodeID().Return(vdr2ID)
	innerMsg4.EXPECT().Op().Return(message.PutOp).AnyTimes()
	msg4 := Message{
		InboundMessage: innerMsg4,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}

	ctx1, ctx2, ctx3, ctx4 := context.Background(), context.Background(), context.Background(), context.Background()

	q.Push(ctx1, msg1)
	q.Push(ctx2, msg2)
	q.Push(ctx3, msg3)
	q.Push(ctx4, msg4)

	// both nodes are in the same bucket, so it should cycle between them
	ctx, gotMsg1, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx1, ctx)
	require.Equal(msg1, gotMsg1)

	ctx, gotMsg3, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx3, ctx)
	require.Equal(msg3, gotMsg3)

	ctx, gotMsg2, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx2, ctx)
	require.Equal(msg2, gotMsg2)

	ctx, gotMsg4, ok := q.Pop()
	require.True(ok)
	require.Equal(ctx4, ctx)
	require.Equal(msg4, gotMsg4)
}

func TestMultilevelQueue_UpdateNodePriority(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cpuTargeter := tracker.NewMockTargeter(ctrl)
	cpuTracker := tracker.NewMockTracker(ctrl)
	queueIntf, err := NewMessageQueue(
		logging.NoLog{},
		cpuTracker,
		cpuTargeter,
		"",
		prometheus.NewRegistry(),
		message.SynchronousOps,
	)
	require.NoError(err)
	queue, ok := queueIntf.(*multilevelMessageQueue)
	require.True(ok)

	// Add a queue for [nodeID]
	nodeID := ids.GenerateTestNodeID()
	nodeQueue := newNodeMessageQueue(nodeID)
	queue.utilizatonBuckets[0].addNodeMessageQueue(nodeQueue)

	// Update the node's priority when its usage is higher than the target
	for i := 0; i < len(queue.utilizatonBuckets)-1; i++ {
		cpuTargeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(0)).Times(1)
		cpuTracker.EXPECT().Usage(nodeID, gomock.Any()).Return(float64(1)).Times(1)
		queue.updateNodePriority(nodeQueue)
		require.Len(queue.utilizatonBuckets[i].nodeQueues, 0)
		require.Len(queue.utilizatonBuckets[i+1].nodeQueues, 1)
	}

	// Test that the priority doesn't move when it's in the lowest priority bucket
	cpuTargeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(0)).Times(1)
	cpuTracker.EXPECT().Usage(nodeID, gomock.Any()).Return(float64(1)).Times(1)
	queue.updateNodePriority(nodeQueue)
	require.Len(queue.utilizatonBuckets[len(queue.utilizatonBuckets)-1].nodeQueues, 1)

	// Update the node's priority when its usage is lower than the target
	for i := len(queue.utilizatonBuckets) - 1; i > 0; i-- {
		cpuTargeter.EXPECT().TargetUsage(gomock.Any()).Return(math.MaxFloat64).Times(1)
		cpuTracker.EXPECT().Usage(nodeID, gomock.Any()).Return(float64(0)).Times(1)
		queue.updateNodePriority(nodeQueue)
		require.Len(queue.utilizatonBuckets[i].nodeQueues, 0)
		require.Len(queue.utilizatonBuckets[i-1].nodeQueues, 1)
	}

	// Test that the priority doesn't move when it's in the highest priority bucket
	cpuTargeter.EXPECT().TargetUsage(gomock.Any()).Return(math.MaxFloat64).Times(1)
	cpuTracker.EXPECT().Usage(nodeID, gomock.Any()).Return(float64(0)).Times(1)
	queue.updateNodePriority(nodeQueue)
	require.Len(queue.utilizatonBuckets[0].nodeQueues, 1)
}

// Test that Pop blocks until a message is pushed or the queue is shutdown.
func TestMessageQueuePopBlocking(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cpuTargeter := tracker.NewMockTargeter(ctrl)
	cpuTargeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(0)).AnyTimes()
	cpuTracker := tracker.NewMockTracker(ctrl)
	cpuTracker.EXPECT().Usage(gomock.Any(), gomock.Any()).Return(float64(0)).AnyTimes()
	q, err := NewMessageQueue(
		logging.NoLog{},
		cpuTracker,
		cpuTargeter,
		"",
		prometheus.NewRegistry(),
		message.SynchronousOps,
	)
	require.NoError(err)

	// Create a message to push
	innerMsg := message.NewMockInboundMessage(ctrl)
	innerMsg.EXPECT().NodeID().Return(ids.GenerateTestNodeID()).AnyTimes()
	innerMsg.EXPECT().Op().Return(message.PutOp).AnyTimes()
	ctx := context.Background()
	msg := Message{
		InboundMessage: innerMsg,
		EngineType:     p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	}

	// Wait for the message to be popped in a goroutine
	popReturned := make(chan struct{})
	go func() {
		gotCtx, gotMsg, ok := q.Pop()
		require.True(ok)
		require.Equal(ctx, gotCtx)
		require.Equal(msg, gotMsg)
		close(popReturned)
	}()

	// Make sure Pop doesn't return before the message is pushed
	require.Never(func() bool {
		select {
		case <-popReturned:
			return true
		default:
			return false
		}
	},
		time.Second,
		100*time.Millisecond,
	)

	// Push the message
	q.Push(ctx, msg)

	// Make sure Pop returns after the message is pushed
	require.Eventually(func() bool {
		select {
		case <-popReturned:
			return true
		default:
			return false
		}
	},
		5*time.Second,
		100*time.Millisecond,
	)

	require.Equal(0, q.Len())

	// Similar to above, but test that Pop returns when the queue is shutdown.
	popReturned = make(chan struct{})
	go func() {
		_, _, ok := q.Pop()
		require.False(ok)
		close(popReturned)
	}()

	require.Never(func() bool {
		select {
		case <-popReturned:
			return true
		default:
			return false
		}
	},
		time.Second,
		100*time.Millisecond,
	)

	q.Shutdown()

	require.Eventually(func() bool {
		select {
		case <-popReturned:
			return true
		default:
			return false
		}
	},
		5*time.Second,
		100*time.Millisecond,
	)

}
