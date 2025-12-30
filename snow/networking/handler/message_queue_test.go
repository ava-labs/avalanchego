// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/tracker/trackermock"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	require := require.New(t)
	cpuTracker := trackermock.NewTracker(ctrl)
	vdrs := validators.NewManager()
	vdr1ID, vdr2ID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr2ID, nil, ids.Empty, 1))
	mIntf, err := NewMessageQueue(
		logging.NoLog{},
		constants.PrimaryNetworkID,
		vdrs,
		cpuTracker,
		"",
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	u := mIntf.(*messageQueue)
	currentTime := time.Now()
	u.clock.Set(currentTime)

	msg1 := Message{
		InboundMessage: message.InboundPullQuery(
			ids.Empty,
			0,
			time.Second,
			ids.GenerateTestID(),
			0,
			vdr1ID,
		),
		EngineType: p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}

	// Push then pop should work regardless of usage when there are no other
	// messages on [u.msgs]
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.1).Times(1)
	u.Push(t.Context(), msg1)
	require.Equal(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.Equal(1, u.Len())
	_, gotMsg1, ok := u.Pop()
	require.True(ok)
	require.Empty(u.nodeToUnprocessedMsgs)
	require.Zero(u.Len())
	require.Equal(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)
	u.Push(t.Context(), msg1)
	require.Equal(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.Equal(1, u.Len())
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Empty(u.nodeToUnprocessedMsgs)
	require.Zero(u.Len())
	require.Equal(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(1.0).Times(1)
	u.Push(t.Context(), msg1)
	require.Equal(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.Equal(1, u.Len())
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Empty(u.nodeToUnprocessedMsgs)
	require.Zero(u.Len())
	require.Equal(msg1, gotMsg1)

	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)
	u.Push(t.Context(), msg1)
	require.Equal(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.Equal(1, u.Len())
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Empty(u.nodeToUnprocessedMsgs)
	require.Zero(u.Len())
	require.Equal(msg1, gotMsg1)

	// Push msg1 from vdr1ID
	u.Push(t.Context(), msg1)
	require.Equal(1, u.nodeToUnprocessedMsgs[vdr1ID])
	require.Equal(1, u.Len())

	msg2 := Message{
		InboundMessage: message.InboundPullQuery(
			ids.Empty,
			0,
			time.Second,
			ids.GenerateTestID(),
			0,
			vdr2ID,
		),
		EngineType: p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}

	// Push msg2 from vdr2ID
	u.Push(t.Context(), msg2)
	require.Equal(2, u.Len())
	require.Equal(1, u.nodeToUnprocessedMsgs[vdr2ID])
	// Set vdr1's usage to 99% and vdr2's to .01
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(.99).Times(2)
	cpuTracker.EXPECT().Usage(vdr2ID, gomock.Any()).Return(.01).Times(1)
	// Pop should return msg2 first because vdr1 has exceeded it's portion of CPU time
	_, gotMsg2, ok := u.Pop()
	require.True(ok)
	require.Equal(1, u.Len())
	require.Equal(msg2, gotMsg2)
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Equal(msg1, gotMsg1)
	require.Empty(u.nodeToUnprocessedMsgs)
	require.Zero(u.Len())

	// u is now empty
	// Non-validators should be able to put messages onto [u]
	nonVdrNodeID1, nonVdrNodeID2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	msg3 := Message{
		InboundMessage: message.InboundPullQuery(ids.Empty, 0, 0, ids.Empty, 0, nonVdrNodeID1),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	msg4 := Message{
		InboundMessage: message.InboundPushQuery(ids.Empty, 0, 0, nil, 0, nonVdrNodeID2),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	u.Push(t.Context(), msg3)
	u.Push(t.Context(), msg4)
	u.Push(t.Context(), msg1)
	require.Equal(3, u.Len())

	// msg1 should get popped first because nonVdrNodeID1 and nonVdrNodeID2
	// exceeded their limit
	cpuTracker.EXPECT().Usage(nonVdrNodeID1, gomock.Any()).Return(.34).Times(1)
	cpuTracker.EXPECT().Usage(nonVdrNodeID2, gomock.Any()).Return(.34).Times(2)
	cpuTracker.EXPECT().Usage(vdr1ID, gomock.Any()).Return(0.0).Times(1)

	// u.msgs is [msg3, msg4, msg1]
	_, gotMsg1, ok = u.Pop()
	require.True(ok)
	require.Equal(msg1, gotMsg1)
	// u.msgs is [msg3, msg4]
	cpuTracker.EXPECT().Usage(nonVdrNodeID1, gomock.Any()).Return(.51).Times(2)
	_, gotMsg4, ok := u.Pop()
	require.True(ok)
	require.Equal(msg4, gotMsg4)
	// u.msgs is [msg3]
	_, gotMsg3, ok := u.Pop()
	require.True(ok)
	require.Equal(msg3, gotMsg3)
	require.Zero(u.Len())
}
