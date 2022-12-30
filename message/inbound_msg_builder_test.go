// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func Test_newMsgBuilder(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mb, err := newMsgBuilder(
		"test",
		prometheus.NewRegistry(),
		10*time.Second,
	)
	require.NoError(err)
	require.NotNil(mb)
}

func TestInboundMsgBuilder(t *testing.T) {
	var (
		chainID                     = ids.GenerateTestID()
		requestID            uint32 = 12345
		deadline                    = time.Hour
		nodeID                      = ids.GenerateTestNodeID()
		summary                     = []byte{9, 8, 7}
		appBytes                    = []byte{1, 3, 3, 7}
		container                   = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
		containerIDs                = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		acceptedContainerIDs        = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		summaryIDs                  = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		heights                     = []uint64{1000, 2000}
		engineType                  = p2p.EngineType_ENGINE_TYPE_SNOWMAN
	)

	t.Run(
		"InboundGetStateSummaryFrontier",
		func(t *testing.T) {
			require := require.New(t)

			start := time.Now()
			msg := InboundGetStateSummaryFrontier(
				chainID,
				requestID,
				deadline,
				nodeID,
			)
			end := time.Now()

			require.Equal(GetStateSummaryFrontierOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			innerMsg, ok := msg.Message().(*p2p.GetStateSummaryFrontier)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
		},
	)

	t.Run(
		"InboundStateSummaryFrontier",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundStateSummaryFrontier(
				chainID,
				requestID,
				summary,
				nodeID,
			)

			require.Equal(StateSummaryFrontierOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			innerMsg, ok := msg.Message().(*p2p.StateSummaryFrontier)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(summary, innerMsg.Summary)
		},
	)

	t.Run(
		"InboundGetAcceptedStateSummary",
		func(t *testing.T) {
			require := require.New(t)

			start := time.Now()
			msg := InboundGetAcceptedStateSummary(
				chainID,
				requestID,
				heights,
				deadline,
				nodeID,
			)
			end := time.Now()

			require.Equal(GetAcceptedStateSummaryOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			innerMsg, ok := msg.Message().(*p2p.GetAcceptedStateSummary)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(heights, innerMsg.Heights)
		},
	)

	t.Run(
		"InboundAcceptedStateSummary",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundAcceptedStateSummary(
				chainID,
				requestID,
				summaryIDs,
				nodeID,
			)

			require.Equal(AcceptedStateSummaryOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			innerMsg, ok := msg.Message().(*p2p.AcceptedStateSummary)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			summaryIDsBytes := make([][]byte, len(summaryIDs))
			for i, id := range summaryIDs {
				id := id
				summaryIDsBytes[i] = id[:]
			}
			require.Equal(summaryIDsBytes, innerMsg.SummaryIds)
		},
	)

	t.Run(
		"InboundGetAcceptedFrontier",
		func(t *testing.T) {
			require := require.New(t)

			start := time.Now()
			msg := InboundGetAcceptedFrontier(
				chainID,
				requestID,
				deadline,
				nodeID,
				engineType,
			)
			end := time.Now()

			require.Equal(GetAcceptedFrontierOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			innerMsg, ok := msg.Message().(*p2p.GetAcceptedFrontier)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(engineType, innerMsg.EngineType)
		},
	)

	t.Run(
		"InboundAcceptedFrontier",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundAcceptedFrontier(
				chainID,
				requestID,
				containerIDs,
				nodeID,
				engineType,
			)

			require.Equal(AcceptedFrontierOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			innerMsg, ok := msg.Message().(*p2p.AcceptedFrontier)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			containerIDsBytes := make([][]byte, len(containerIDs))
			for i, id := range containerIDs {
				id := id
				containerIDsBytes[i] = id[:]
			}
			require.Equal(containerIDsBytes, innerMsg.ContainerIds)
			require.Equal(engineType, innerMsg.EngineType)
		},
	)

	t.Run(
		"InboundGetAccepted",
		func(t *testing.T) {
			require := require.New(t)

			start := time.Now()
			msg := InboundGetAccepted(
				chainID,
				requestID,
				deadline,
				containerIDs,
				nodeID,
				engineType,
			)
			end := time.Now()

			require.Equal(GetAcceptedOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			innerMsg, ok := msg.Message().(*p2p.GetAccepted)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(engineType, innerMsg.EngineType)
		},
	)

	t.Run(
		"InboundAccepted",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundAccepted(
				chainID,
				requestID,
				containerIDs,
				nodeID,
				engineType,
			)

			require.Equal(AcceptedOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			innerMsg, ok := msg.Message().(*p2p.Accepted)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			containerIDsBytes := make([][]byte, len(containerIDs))
			for i, id := range containerIDs {
				id := id
				containerIDsBytes[i] = id[:]
			}
			require.Equal(containerIDsBytes, innerMsg.ContainerIds)
			require.Equal(engineType, innerMsg.EngineType)
		},
	)

	t.Run(
		"InboundPushQuery",
		func(t *testing.T) {
			require := require.New(t)

			start := time.Now()
			msg := InboundPushQuery(
				chainID,
				requestID,
				deadline,
				container,
				nodeID,
				engineType,
			)
			end := time.Now()

			require.Equal(PushQueryOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			innerMsg, ok := msg.Message().(*p2p.PushQuery)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(container, innerMsg.Container)
			require.Equal(engineType, innerMsg.EngineType)
		},
	)

	t.Run(
		"InboundPullQuery",
		func(t *testing.T) {
			require := require.New(t)

			start := time.Now()
			msg := InboundPullQuery(
				chainID,
				requestID,
				deadline,
				containerIDs[0],
				nodeID,
				engineType,
			)
			end := time.Now()

			require.Equal(PullQueryOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			innerMsg, ok := msg.Message().(*p2p.PullQuery)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(containerIDs[0][:], innerMsg.ContainerId)
			require.Equal(engineType, innerMsg.EngineType)
		},
	)

	t.Run(
		"InboundChits",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundChits(
				chainID,
				requestID,
				containerIDs,
				acceptedContainerIDs,
				nodeID,
				engineType,
			)

			require.Equal(ChitsOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			innerMsg, ok := msg.Message().(*p2p.Chits)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			containerIDsBytes := make([][]byte, len(containerIDs))
			for i, id := range containerIDs {
				id := id
				containerIDsBytes[i] = id[:]
			}
			require.Equal(containerIDsBytes, innerMsg.PreferredContainerIds)
			acceptedContainerIDsBytes := make([][]byte, len(acceptedContainerIDs))
			for i, id := range acceptedContainerIDs {
				id := id
				acceptedContainerIDsBytes[i] = id[:]
			}
			require.Equal(acceptedContainerIDsBytes, innerMsg.AcceptedContainerIds)
			require.Equal(engineType, innerMsg.EngineType)
		},
	)

	t.Run(
		"InboundAppRequest",
		func(t *testing.T) {
			require := require.New(t)

			start := time.Now()
			msg := InboundAppRequest(
				chainID,
				requestID,
				deadline,
				appBytes,
				nodeID,
			)
			end := time.Now()

			require.Equal(AppRequestOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			innerMsg, ok := msg.Message().(*p2p.AppRequest)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(appBytes, innerMsg.AppBytes)
		},
	)

	t.Run(
		"InboundAppResponse",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundAppResponse(
				chainID,
				requestID,
				appBytes,
				nodeID,
			)

			require.Equal(AppResponseOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			innerMsg, ok := msg.Message().(*p2p.AppResponse)
			require.True(ok)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(appBytes, innerMsg.AppBytes)
		},
	)
}
