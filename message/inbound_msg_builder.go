// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ InboundMsgBuilder = (*inMsgBuilder)(nil)

type InboundMsgBuilder interface {
	// Parse reads given bytes as InboundMessage
	Parse(
		bytes []byte,
		nodeID ids.NodeID,
		onFinishedHandling func(),
	) (*InboundMessage, error)
}

type inMsgBuilder struct {
	builder *msgBuilder
}

func newInboundBuilder(builder *msgBuilder) InboundMsgBuilder {
	return &inMsgBuilder{
		builder: builder,
	}
}

func (b *inMsgBuilder) Parse(bytes []byte, nodeID ids.NodeID, onFinishedHandling func()) (*InboundMessage, error) {
	return b.builder.parseInbound(bytes, nodeID, onFinishedHandling)
}

func InboundGetStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     GetStateSummaryFrontierOp,
		Message: &p2p.GetStateSummaryFrontier{
			ChainId:   chainID[:],
			RequestId: requestID,
			Deadline:  uint64(deadline),
		},
		Expiration: time.Now().Add(deadline),
	}
}

func InboundStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	summary []byte,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     StateSummaryFrontierOp,
		Message: &p2p.StateSummaryFrontier{
			ChainId:   chainID[:],
			RequestId: requestID,
			Summary:   summary,
		},
		Expiration: mockable.MaxTime,
	}
}

func InboundGetAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	heights []uint64,
	deadline time.Duration,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     GetAcceptedStateSummaryOp,
		Message: &p2p.GetAcceptedStateSummary{
			ChainId:   chainID[:],
			RequestId: requestID,
			Deadline:  uint64(deadline),
			Heights:   heights,
		},
		Expiration: time.Now().Add(deadline),
	}
}

func InboundAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	summaryIDs []ids.ID,
	nodeID ids.NodeID,
) *InboundMessage {
	summaryIDBytes := make([][]byte, len(summaryIDs))
	encodeIDs(summaryIDs, summaryIDBytes)
	return &InboundMessage{
		NodeID: nodeID,
		Op:     AcceptedStateSummaryOp,
		Message: &p2p.AcceptedStateSummary{
			ChainId:    chainID[:],
			RequestId:  requestID,
			SummaryIds: summaryIDBytes,
		},
		Expiration: mockable.MaxTime,
	}
}

func InboundGetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     GetAcceptedFrontierOp,
		Message: &p2p.GetAcceptedFrontier{
			ChainId:   chainID[:],
			RequestId: requestID,
			Deadline:  uint64(deadline),
		},
		Expiration: time.Now().Add(deadline),
	}
}

func InboundAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     AcceptedFrontierOp,
		Message: &p2p.AcceptedFrontier{
			ChainId:     chainID[:],
			RequestId:   requestID,
			ContainerId: containerID[:],
		},
		Expiration: mockable.MaxTime,
	}
}

func InboundGetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) *InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &InboundMessage{
		NodeID: nodeID,
		Op:     GetAcceptedOp,
		Message: &p2p.GetAccepted{
			ChainId:      chainID[:],
			RequestId:    requestID,
			Deadline:     uint64(deadline),
			ContainerIds: containerIDBytes,
		},
		Expiration: time.Now().Add(deadline),
	}
}

func InboundAccepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) *InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &InboundMessage{
		NodeID: nodeID,
		Op:     AcceptedOp,
		Message: &p2p.Accepted{
			ChainId:      chainID[:],
			RequestId:    requestID,
			ContainerIds: containerIDBytes,
		},
		Expiration: mockable.MaxTime,
	}
}

func InboundPushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	container []byte,
	requestedHeight uint64,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     PushQueryOp,
		Message: &p2p.PushQuery{
			ChainId:         chainID[:],
			RequestId:       requestID,
			Deadline:        uint64(deadline),
			Container:       container,
			RequestedHeight: requestedHeight,
		},
		Expiration: time.Now().Add(deadline),
	}
}

func InboundPullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	requestedHeight uint64,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     PullQueryOp,
		Message: &p2p.PullQuery{
			ChainId:         chainID[:],
			RequestId:       requestID,
			Deadline:        uint64(deadline),
			ContainerId:     containerID[:],
			RequestedHeight: requestedHeight,
		},
		Expiration: time.Now().Add(deadline),
	}
}

func InboundChits(
	chainID ids.ID,
	requestID uint32,
	preferredID ids.ID,
	preferredIDAtHeight ids.ID,
	acceptedID ids.ID,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     ChitsOp,
		Message: &p2p.Chits{
			ChainId:             chainID[:],
			RequestId:           requestID,
			PreferredId:         preferredID[:],
			PreferredIdAtHeight: preferredIDAtHeight[:],
			AcceptedId:          acceptedID[:],
		},
		Expiration: mockable.MaxTime,
	}
}

func InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     AppRequestOp,
		Message: &p2p.AppRequest{
			ChainId:   chainID[:],
			RequestId: requestID,
			Deadline:  uint64(deadline),
			AppBytes:  msg,
		},
		Expiration: time.Now().Add(deadline),
	}
}

func InboundAppError(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	errorCode int32,
	errorMessage string,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     AppErrorOp,
		Message: &p2p.AppError{
			ChainId:      chainID[:],
			RequestId:    requestID,
			ErrorCode:    errorCode,
			ErrorMessage: errorMessage,
		},
		Expiration: mockable.MaxTime,
	}
}

func InboundAppResponse(
	chainID ids.ID,
	requestID uint32,
	msg []byte,
	nodeID ids.NodeID,
) *InboundMessage {
	return &InboundMessage{
		NodeID: nodeID,
		Op:     AppResponseOp,
		Message: &p2p.AppResponse{
			ChainId:   chainID[:],
			RequestId: requestID,
			AppBytes:  msg,
		},
		Expiration: mockable.MaxTime,
	}
}

// InboundSimplexMessage creates a new InboundMessage for simplex messages.
func InboundSimplexMessage(
	nodeID ids.NodeID,
	msg *p2p.Simplex,
) *InboundMessage {
	return &InboundMessage{
		NodeID:     nodeID,
		Op:         SimplexOp,
		Message:    msg,
		Expiration: mockable.MaxTime,
	}
}

func encodeIDs(ids []ids.ID, result [][]byte) {
	for i, id := range ids {
		result[i] = id[:]
	}
}
