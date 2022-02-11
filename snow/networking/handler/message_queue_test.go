// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestQueue(t *testing.T) {
	assert := assert.New(t)
	cpuTracker := &tracker.MockTimeTracker{}
	vdrs := validators.NewSet()
	vdr1ID, vdr2ID := ids.GenerateTestShortID(), ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	assert.NoError(vdrs.AddWeight(vdr2ID, 1))
	mIntf, err := NewMessageQueue(logging.NoLog{}, vdrs, cpuTracker, "", prometheus.NewRegistry())
	assert.NoError(err)
	u := mIntf.(*messageQueue)
	currentTime := time.Now()
	u.clock.Set(currentTime)

	mc, err := message.NewCreator(prometheus.NewRegistry(), true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(err)
	mc.SetTime(currentTime)
	msg1 := mc.InboundPut(ids.Empty,
		0,
		ids.GenerateTestID(),
		nil,
		vdr1ID,
	)

	// Push then pop should work regardless of utilization when there are
	// no other messages on [u.msgs]
	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.1).Once()
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1, ok := u.Pop()
	assert.True(ok)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.0).Once()
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(1.0).Once()
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.0).Once()
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
	// Set vdr1's CPU utilization to 99% and vdr2's to .01
	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(.99).Twice()
	cpuTracker.On("Utilization", vdr2ID, mock.Anything).Return(.01).Once()
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
	nonVdrNodeID1, nonVdrNodeID2 := ids.GenerateTestShortID(), ids.GenerateTestShortID()
	msg3 := mc.InboundPullQuery(ids.Empty, 0, 0, ids.Empty, nonVdrNodeID1)
	msg4 := mc.InboundPushQuery(ids.Empty, 0, 0, ids.Empty, nil, nonVdrNodeID2)
	u.Push(msg3)
	u.Push(msg4)
	u.Push(msg1)
	assert.EqualValues(3, u.Len())

	// msg1 should get popped first because nonVdrNodeID1 and nonVdrNodeID2
	// exceeded their limit
	cpuTracker.On("Utilization", nonVdrNodeID1, mock.Anything).Return(.34).Once()
	cpuTracker.On("Utilization", nonVdrNodeID2, mock.Anything).Return(.34).Times(3)
	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.0).Once()

	// u.msgs is [msg3, msg4, msg1]
	gotMsg1, ok = u.Pop()
	assert.True(ok)
	assert.EqualValues(msg1, gotMsg1)
	// u.msgs is [msg3, msg4]
	cpuTracker.On("Utilization", nonVdrNodeID1, mock.Anything).Return(.51).Twice()
	gotMsg4, ok := u.Pop()
	assert.True(ok)
	assert.EqualValues(msg4, gotMsg4)
	// u.msgs is [msg3]
	gotMsg3, ok := u.Pop()
	assert.True(ok)
	assert.EqualValues(msg3, gotMsg3)
	assert.EqualValues(0, u.Len())
}
