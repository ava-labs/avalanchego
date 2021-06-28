// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestUnprocessedMsgs(t *testing.T) {
	assert := assert.New(t)
	cpuTracker := &tracker.MockTimeTracker{}
	vdrs := validators.NewSet()
	vdr1ID, vdr2ID := ids.GenerateTestShortID(), ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	assert.NoError(vdrs.AddWeight(vdr2ID, 1))
	uIntf := newUnprocessedMsgs(logging.NoLog{}, vdrs, cpuTracker)
	u := uIntf.(*unprocessedMsgsImpl)
	msg1 := message{
		messageType: constants.PutMsg,
		nodeID:      vdr1ID,
	}

	// Push then pop should work regardless of utilization
	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.1).Once()
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1 := u.Pop()
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.0).Once()
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1 = u.Pop()
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(1.0).Once()
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1 = u.Pop()
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.0).Once()
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())
	gotMsg1 = u.Pop()
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())
	assert.EqualValues(msg1, gotMsg1)

	// Push msg1 from vdr1ID
	u.Push(msg1)
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr1ID])
	assert.EqualValues(1, u.Len())

	msg2 := message{
		messageType: constants.GetMsg,
		nodeID:      vdr2ID,
	}
	// Push msg2 from vdr2ID
	u.Push(msg2)
	assert.EqualValues(2, u.Len())
	assert.EqualValues(1, u.nodeToUnprocessedMsgs[vdr2ID])
	// Set vdr1's CPU utilization to 99% and vdr2's to .01
	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(.99).Twice()
	cpuTracker.On("Utilization", vdr2ID, mock.Anything).Return(.01).Once()
	// Pop should return msg2 first because vdr1 has exceeded it's portion of CPU time
	gotMsg2 := u.Pop()
	assert.EqualValues(1, u.Len())
	assert.EqualValues(msg2, gotMsg2)
	gotMsg1 = u.Pop()
	assert.EqualValues(msg1, gotMsg1)
	assert.Len(u.nodeToUnprocessedMsgs, 0)
	assert.EqualValues(0, u.Len())

	// u is now empty
	// Non-validators should be able to put messages onto [u]
	nonVdrNodeID1, nonVdrNodeID2 := ids.GenerateTestShortID(), ids.GenerateTestShortID()
	msg3 := message{
		messageType: constants.PullQueryMsg,
		nodeID:      nonVdrNodeID1,
	}
	msg4 := message{
		messageType: constants.PushQueryMsg,
		nodeID:      nonVdrNodeID2,
	}
	u.Push(msg3)
	u.Push(msg4)
	u.Push(msg1)
	assert.EqualValues(3, u.Len())

	// msg1 should get popped first because nonVdrNodeID1 and nonVdrNodeID2
	// exceeded their limit
	cpuTracker.On("Utilization", nonVdrNodeID1, mock.Anything).Return(.34).Twice()
	cpuTracker.On("Utilization", nonVdrNodeID2, mock.Anything).Return(.34).Times(3)
	cpuTracker.On("Utilization", vdr1ID, mock.Anything).Return(0.0).Once()

	// u.msgs is [msg3, msg4, msg1]
	gotMsg1 = u.Pop()
	assert.EqualValues(msg1, gotMsg1)
	// u.msgs is [msg3, msg4]
	gotMsg3 := u.Pop()
	assert.EqualValues(msg3, gotMsg3)
	// u.msgs is [msg4]
	gotMsg4 := u.Pop()
	assert.EqualValues(msg4, gotMsg4)
	assert.EqualValues(0, u.Len())
}
