// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	require := require.New(t)
	cpuTracker := tracker.NewMockTracker(ctrl)
	vdrs := validators.NewSet()
	vdr1ID, vdr2ID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	require.NoError(vdrs.AddWeight(vdr1ID, 1))
	require.NoError(vdrs.AddWeight(vdr2ID, 1))
	mIntf, err := NewMessageQueue(logging.NoLog{}, vdrs, cpuTracker, "", prometheus.NewRegistry(), message.SynchronousOps)
	require.NoError(err)
	u := mIntf.(*messageQueue)
	currentTime := time.Now()
	u.clock.Set(currentTime)

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(err)

	mc.SetTime(currentTime)
	msg1 := mc.InboundPut(
		ids.Empty,
		0,
		nil,
		vdr1ID,
	)

	// Push then pop should work regardless of usage when there are no other
	// messages on [u.msgs]
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.1).Times(1)
	u.Push(context.Background(), msg1)
	require.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.EqualValues(1, u.Len())
	_, gotMsg1, ok := u.Pop()
	require.True(ok)
	require.Len(u.nodeToUnprocessedMsgs, 0)
	require.EqualValues(0, u.Len())
	require.EqualValues(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)
	u.Push(context.Background(), msg1)
	require.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.EqualValues(1, u.Len())
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Len(u.nodeToUnprocessedMsgs, 0)
	require.EqualValues(0, u.Len())
	require.EqualValues(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(1.0).Times(1)
	u.Push(context.Background(), msg1)
	require.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.EqualValues(1, u.Len())
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Len(u.nodeToUnprocessedMsgs, 0)
	require.EqualValues(0, u.Len())
	require.EqualValues(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)
	u.Push(context.Background(), msg1)
	require.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.EqualValues(1, u.Len())
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Len(u.nodeToUnprocessedMsgs, 0)
	require.EqualValues(0, u.Len())
	require.EqualValues(msg1, gotMsg1)

	// Push msg1 from vdr1ID
	u.Push(context.Background(), msg1)
	require.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.EqualValues(1, u.Len())

	msg2 := mc.InboundGet(ids.Empty, 0, 0, ids.Empty, vdr2ID)

	// Push msg2 from vdr2ID
	u.Push(context.Background(), msg2)
	require.EqualValues(2, u.Len())
	require.EqualValues(1, u.nodeToUnprocessedMsgs[vdr2ID])
	// Set vdr1's usage to 99% and vdr2's to .01
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(.99).Times(2)
	cpuTracker.EXPECT().Usage(vdr2ID, gomock.Any()).Return(.01).Times(1)
	// Pop should return msg2 first because vdr1 has exceeded it's portion of CPU time
	_, gotMsg2, ok := u.Pop()
	require.True(ok)
	require.EqualValues(1, u.Len())
	require.EqualValues(msg2, gotMsg2)
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.EqualValues(msg1, gotMsg1)
	require.Len(u.nodeToUnprocessedMsgs, 0)
	require.EqualValues(0, u.Len())

	// u is now empty
	// Non-validators should be able to put messages onto [u]
	nonVdrNodeID1, nonVdrNodeID2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	msg3 := mc.InboundPullQuery(ids.Empty, 0, 0, ids.Empty, nonVdrNodeID1)
	msg4 := mc.InboundPushQuery(ids.Empty, 0, 0, nil, nonVdrNodeID2)
	u.Push(context.Background(), msg3)
	u.Push(context.Background(), msg4)
	u.Push(context.Background(), msg1)
	require.EqualValues(3, u.Len())

	// msg1 should get popped first because nonVdrNodeID1 and nonVdrNodeID2
	// exceeded their limit
	cpuTracker.EXPECT().Usage(nonVdrNodeID1, gomock.Any()).Return(.34).Times(1)
	cpuTracker.EXPECT().Usage(nonVdrNodeID2, gomock.Any()).Return(.34).Times(2)
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)

	// u.msgs is [msg3, msg4, msg1]
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.EqualValues(msg1, gotMsg1)
	// u.msgs is [msg3, msg4]
	cpuTracker.EXPECT().Usage(nonVdrNodeID1, gomock.Any()).Return(.51).Times(2)
	_, gotMsg4, ok := u.Pop()
	require.True(ok)
	require.EqualValues(msg4, gotMsg4)
	// u.msgs is [msg3]
	_, gotMsg3, ok := u.Pop()
	require.True(ok)
	require.EqualValues(msg3, gotMsg3)
	require.EqualValues(0, u.Len())
}
