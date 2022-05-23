// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	assert := assert.New(t)
	cpuTracker := tracker.NewMockTracker(ctrl)
	vdrs := validators.NewSet()
	vdr1ID, vdr2ID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	assert.NoError(vdrs.AddWeight(vdr2ID, 1))
	mIntf, err := NewMessageQueue(logging.NoLog{}, vdrs, cpuTracker, "", prometheus.NewRegistry(), message.SynchronousOps)
	assert.NoError(err)
	u := mIntf.(*messageQueue)
	currentTime := time.Now()
	u.clock.Set(currentTime)

	mc, err := message.NewCreator(prometheus.NewRegistry(), true, "dummyNamespace", 10*time.Second)
	assert.NoError(err)
	mc.SetTime(currentTime)
	msg1 := mc.InboundPut(ids.Empty,
		0,
		ids.GenerateTestID(),
		nil,
		vdr1ID,
	)

	// Push then pop should work regardless of usage when there are no other
	// messages on [u.msgs]
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.1).Times(1)
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1, ok := u.Pop()
	assert.True(ok)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(1.0).Times(1)
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	// Push msg1 from vdr1ID
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())

	msg2 := mc.InboundGet(ids.Empty, 0, 0, ids.Empty, vdr2ID)

	// Push msg2 from vdr2ID
	u.Push(msg2)
	assert.EqualValues(2, u.Len())
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr2ID])
	// Set vdr1's usage to 99% and vdr2's to .01
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(.99).Times(2)
	cpuTracker.EXPECT().Usage(vdr2ID, gomock.Any()).Return(.01).Times(1)
	// Pop should return msg2 first because vdr1 has exceeded it's portion of CPU time
	gotMsg2, ok := u.Pop()
	assert.True(ok)
	assert.EqualValues(1, u.Len())
	assert.EqualValues(msg2, gotMsg2)
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.EqualValues(msg1, gotMsg1)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())

	// u is now empty
	// Non-validators should be able to put messages onto [u]
	nonVdrNodeID1, nonVdrNodeID2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	msg3 := mc.InboundPullQuery(ids.Empty, 0, 0, ids.Empty, nonVdrNodeID1)
	msg4 := mc.InboundPushQuery(ids.Empty, 0, 0, ids.Empty, nil, nonVdrNodeID2)
	u.Push(msg3)
	u.Push(msg4)
	u.Push(msg1)
	assert.EqualValues(3, u.Len())

	// msg1 should get popped first because nonVdrNodeID1 and nonVdrNodeID2
	// exceeded their limit
	cpuTracker.EXPECT().Usage(nonVdrNodeID1, gomock.Any()).Return(.34).Times(1)
	cpuTracker.EXPECT().Usage(nonVdrNodeID2, gomock.Any()).Return(.34).Times(2)
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)

	// u.msgs is [msg3, msg4, msg1]
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.EqualValues(msg1, gotMsg1)
	// u.msgs is [msg3, msg4]
	cpuTracker.EXPECT().Usage(nonVdrNodeID1, gomock.Any()).Return(.51).Times(2)
	gotMsg4, ok := u.Pop()
	assert.True(ok)
	assert.EqualValues(msg4, gotMsg4)
	// u.msgs is [msg3]
	gotMsg3, ok := u.Pop()
	assert.True(ok)
	assert.EqualValues(msg3, gotMsg3)
	assert.EqualValues(0, u.Len())
}
