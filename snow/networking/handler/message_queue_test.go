// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package handler

import (
	"context"
	"math"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// func TestQueue_FIFO(t *testing.T) {
// 	ctrl := gomock.NewController(t)
// 	defer ctrl.Finish()

// 	require := require.New(t)
// 	cpuTracker := tracker.NewMockTracker(ctrl)
// 	vdrs := validators.NewSet()
// 	vdr1ID, vdr2ID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
// 	require.NoError(vdrs.AddWeight(vdr1ID, 1))
// 	require.NoError(vdrs.AddWeight(vdr2ID, 1))
// 	targeter := tracker.NewMockTargeter(ctrl)
// 	targeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(1)).AnyTimes()
// 	cpuTracker.EXPECT().Usage(gomock.Any(), gomock.Any()).Return(float64(1)).AnyTimes()
// 	mIntf, err := NewMessageQueue(logging.NoLog{}, cpuTracker, targeter, "", prometheus.NewRegistry(), message.SynchronousOps)
// 	require.NoError(err)
// 	u := mIntf.(*multilevelMessageQueue)
// 	currentTime := time.Now()
// 	u.clock.Set(currentTime)

// 	mc, err := message.NewCreator(prometheus.NewRegistry(), "dummyNamespace", true, 10*time.Second)
// 	require.NoError(err)

// 	mc.SetTime(currentTime)
// 	msg1 := mc.InboundPut(
// 		ids.Empty,
// 		0,
// 		nil,
// 		vdr1ID,
// 	)
// 	msg2 := mc.InboundPut(
// 		ids.Empty,
// 		0,
// 		nil,
// 		vdr1ID,
// 	)
// 	msg3 := mc.InboundPut(ids.Empty,
// 		0,
// 		nil,
// 		vdr1ID,
// 	)

// 	u.Push(msg1)
// 	require.EqualValues(1, u.Len())

// 	u.Push(msg2)
// 	require.EqualValues(2, u.Len())

// 	u.Push(msg3)
// 	require.EqualValues(3, u.Len())

// 	// from a single node, it should be FIFO
// 	gotMsg1, ok := u.Pop()
// 	require.True(ok)
// 	require.EqualValues(msg1, gotMsg1)
// 	require.EqualValues(2, u.Len())

// 	gotMsg2, ok := u.Pop()
// 	require.True(ok)
// 	require.EqualValues(msg2, gotMsg2)
// 	require.EqualValues(1, u.Len())

// 	gotMsg3, ok := u.Pop()
// 	require.True(ok)
// 	require.EqualValues(msg3, gotMsg3)
// 	require.EqualValues(0, u.Len())
// }

// func TestQueue_Multiple_Buckets(t *testing.T) {
// 	require := require.New(t)
// 	ctrl := gomock.NewController(t)
// 	defer ctrl.Finish()

// 	// There are 2 nodes.
// 	vdr1ID, vdr2ID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

// 	// Both nodes have target usage of 1.
// 	targeter := tracker.NewMockTargeter(ctrl)
// 	targeter.EXPECT().TargetUsage(gomock.Any()).Return(float64(1)).AnyTimes()

// 	// [vdr1ID] has a usage above the target usage.
// 	// [vdr2ID] has a usage below the target usage.
// 	cpuTracker := tracker.NewMockTracker(ctrl)
// 	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(float64(2)).AnyTimes()
// 	cpuTracker.EXPECT().Usage(vdr2ID, gomock.Any()).Return(float64(0)).AnyTimes()
// 	mIntf, err := NewMessageQueue(logging.NoLog{}, cpuTracker, targeter, "", prometheus.NewRegistry(), message.SynchronousOps)
// 	require.NoError(err)
// 	u := mIntf.(*multilevelMessageQueue)
// 	currentTime := time.Now()
// 	u.clock.Set(currentTime)

// 	// Append one message from each node.
// 	mc, err := message.NewCreator(prometheus.NewRegistry(), "dummyNamespace", true, 10*time.Second)
// 	require.NoError(err)
// 	mc.SetTime(currentTime)

// 	msg1 := mc.InboundPut(
// 		ids.Empty,
// 		1,
// 		nil,
// 		vdr1ID,
// 	)
// 	msg2 := mc.InboundPut(
// 		ids.Empty,
// 		2,
// 		nil,
// 		vdr2ID,
// 	)
// 	msgs := []message.InboundMessage{msg1, msg2}

// 	u.Push(msg1)
// 	u.Push(msg2)

// 	// Make sure we get both messages.
// 	gotMsg1, ok := u.Pop()
// 	require.True(ok)
// 	require.Contains(msgs, gotMsg1)

// 	gotMsg2, ok := u.Pop()
// 	require.True(ok)
// 	require.Contains(msgs, gotMsg2)

// 	require.NotEqual(gotMsg1, gotMsg2)
// }

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
