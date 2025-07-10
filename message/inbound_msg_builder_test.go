// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func Test_newMsgBuilder(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mb, err := newMsgBuilder(
		prometheus.NewRegistry(),
		10*time.Second,
	)
	require.NoError(err)
	require.NotNil(mb)
}

func TestInboundMsgBuilder(t *testing.T) {
	var (
		chainID                    = ids.GenerateTestID()
		requestID           uint32 = 12345
		deadline                   = time.Hour
		nodeID                     = ids.GenerateTestNodeID()
		summary                    = []byte{9, 8, 7}
		appBytes                   = []byte{1, 3, 3, 7}
		container                  = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
		containerIDs               = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		requestedHeight     uint64 = 999
		acceptedContainerID        = ids.GenerateTestID()
		summaryIDs                 = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		heights                    = []uint64{1000, 2000}
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
			require.IsType(&p2p.GetStateSummaryFrontier{}, msg.Message())
			innerMsg := msg.Message().(*p2p.GetStateSummaryFrontier)
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
			require.IsType(&p2p.StateSummaryFrontier{}, msg.Message())
			innerMsg := msg.Message().(*p2p.StateSummaryFrontier)
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
			require.IsType(&p2p.GetAcceptedStateSummary{}, msg.Message())
			innerMsg := msg.Message().(*p2p.GetAcceptedStateSummary)
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
			require.IsType(&p2p.AcceptedStateSummary{}, msg.Message())
			innerMsg := msg.Message().(*p2p.AcceptedStateSummary)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			summaryIDsBytes := make([][]byte, len(summaryIDs))
			for i, id := range summaryIDs {
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
			)
			end := time.Now()

			require.Equal(GetAcceptedFrontierOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			require.IsType(&p2p.GetAcceptedFrontier{}, msg.Message())
			innerMsg := msg.Message().(*p2p.GetAcceptedFrontier)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
		},
	)

	t.Run(
		"InboundAcceptedFrontier",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundAcceptedFrontier(
				chainID,
				requestID,
				containerIDs[0],
				nodeID,
			)

			require.Equal(AcceptedFrontierOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			require.IsType(&p2p.AcceptedFrontier{}, msg.Message())
			innerMsg := msg.Message().(*p2p.AcceptedFrontier)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(containerIDs[0][:], innerMsg.ContainerId)
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
			)
			end := time.Now()

			require.Equal(GetAcceptedOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			require.IsType(&p2p.GetAccepted{}, msg.Message())
			innerMsg := msg.Message().(*p2p.GetAccepted)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
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
			)

			require.Equal(AcceptedOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			require.IsType(&p2p.Accepted{}, msg.Message())
			innerMsg := msg.Message().(*p2p.Accepted)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			containerIDsBytes := make([][]byte, len(containerIDs))
			for i, id := range containerIDs {
				containerIDsBytes[i] = id[:]
			}
			require.Equal(containerIDsBytes, innerMsg.ContainerIds)
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
				requestedHeight,
				nodeID,
			)
			end := time.Now()

			require.Equal(PushQueryOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			require.IsType(&p2p.PushQuery{}, msg.Message())
			innerMsg := msg.Message().(*p2p.PushQuery)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(container, innerMsg.Container)
			require.Equal(requestedHeight, innerMsg.RequestedHeight)
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
				requestedHeight,
				nodeID,
			)
			end := time.Now()

			require.Equal(PullQueryOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.False(msg.Expiration().Before(start.Add(deadline)))
			require.False(end.Add(deadline).Before(msg.Expiration()))
			require.IsType(&p2p.PullQuery{}, msg.Message())
			innerMsg := msg.Message().(*p2p.PullQuery)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(containerIDs[0][:], innerMsg.ContainerId)
			require.Equal(requestedHeight, innerMsg.RequestedHeight)
		},
	)

	t.Run(
		"InboundChits",
		func(t *testing.T) {
			require := require.New(t)

			msg := InboundChits(
				chainID,
				requestID,
				containerIDs[0],
				containerIDs[1],
				acceptedContainerID,
				nodeID,
			)

			require.Equal(ChitsOp, msg.Op())
			require.Equal(nodeID, msg.NodeID())
			require.Equal(mockable.MaxTime, msg.Expiration())
			require.IsType(&p2p.Chits{}, msg.Message())
			innerMsg := msg.Message().(*p2p.Chits)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(containerIDs[0][:], innerMsg.PreferredId)
			require.Equal(containerIDs[1][:], innerMsg.PreferredIdAtHeight)
			require.Equal(acceptedContainerID[:], innerMsg.AcceptedId)
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
			require.IsType(&p2p.AppRequest{}, msg.Message())
			innerMsg := msg.Message().(*p2p.AppRequest)
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
			require.IsType(&p2p.AppResponse{}, msg.Message())
			innerMsg := msg.Message().(*p2p.AppResponse)
			require.Equal(chainID[:], innerMsg.ChainId)
			require.Equal(requestID, innerMsg.RequestId)
			require.Equal(appBytes, innerMsg.AppBytes)
		},
	)
}

func TestAppError(t *testing.T) {
	require := require.New(t)

	mb, err := newMsgBuilder(
		prometheus.NewRegistry(),
		time.Second,
	)
	require.NoError(err)

	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()
	requestID := uint32(1)
	errorCode := int32(2)
	errorMessage := "hello world"

	want := &p2p.Message{
		Message: &p2p.Message_AppError{
			AppError: &p2p.AppError{
				ChainId:      chainID[:],
				RequestId:    requestID,
				ErrorCode:    errorCode,
				ErrorMessage: errorMessage,
			},
		},
	}

	outMsg, err := mb.createOutbound(want, compression.TypeNone, false)
	require.NoError(err)

	got, err := mb.parseInbound(outMsg.Bytes(), nodeID, func() {})
	require.NoError(err)

	require.Equal(nodeID, got.NodeID())
	require.Equal(AppErrorOp, got.Op())

	msg, ok := got.Message().(*p2p.AppError)
	require.True(ok)
	require.Equal(errorCode, msg.ErrorCode)
	require.Equal(errorMessage, msg.ErrorMessage)
}
