// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var _ InboundMsgBuilder = (*inMsgBuilder)(nil)

type InboundMsgBuilder interface {
	SetTime(t time.Time) // useful in UTs

	// Parse reads given bytes as InboundMessage
	// Overrides client specified deadline in a message to maxDeadlineDuration
	Parse(bytes []byte, nodeID ids.NodeID, onFinishedHandling func()) (InboundMessage, error)

	InboundGetStateSummaryFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		nodeID ids.NodeID,
	) InboundMessage

	InboundStateSummaryFrontier(
		chainID ids.ID,
		requestID uint32,
		summary []byte,
		nodeID ids.NodeID,
	) InboundMessage

	InboundGetAcceptedStateSummary(
		chainID ids.ID,
		requestID uint32,
		heights []uint64,
		deadline time.Duration,
		nodeID ids.NodeID,
	) InboundMessage

	InboundAcceptedStateSummary(
		chainID ids.ID,
		requestID uint32,
		summaryIDs []ids.ID,
		nodeID ids.NodeID,
	) InboundMessage

	InboundGetAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		nodeID ids.NodeID,
	) InboundMessage

	InboundAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		nodeID ids.NodeID,
	) InboundMessage

	InboundGetAccepted(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerIDs []ids.ID,
		nodeID ids.NodeID,
	) InboundMessage

	InboundAccepted(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		nodeID ids.NodeID,
	) InboundMessage

	InboundAncestors(
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
		nodeID ids.NodeID,
	) InboundMessage // used in UTs only

	InboundPushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		container []byte,
		nodeID ids.NodeID,
	) InboundMessage

	InboundPullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
		nodeID ids.NodeID,
	) InboundMessage

	InboundChits(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		nodeID ids.NodeID,
	) InboundMessage

	InboundAppRequest(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		msg []byte,
		nodeID ids.NodeID,
	) InboundMessage

	InboundAppResponse(
		chainID ids.ID,
		requestID uint32,
		msg []byte,
		nodeID ids.NodeID,
	) InboundMessage

	InboundGet(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
		nodeID ids.NodeID,
	) InboundMessage

	InboundPut(
		chainID ids.ID,
		requestID uint32,
		container []byte,
		nodeID ids.NodeID,
	) InboundMessage // used in UTs only
}

type inMsgBuilder struct {
	builder *msgBuilder
}

// Use "message.NewCreator" to import this function
// since we do not expose "msgBuilder" yet
func newInboundBuilder(builder *msgBuilder) InboundMsgBuilder {
	return &inMsgBuilder{
		builder: builder,
	}
}

func (b *inMsgBuilder) SetTime(t time.Time) {
	b.builder.clock.Set(t)
}

func (b *inMsgBuilder) Parse(bytes []byte, nodeID ids.NodeID, onFinishedHandling func()) (InboundMessage, error) {
	return b.builder.parseInbound(bytes, nodeID, onFinishedHandling)
}

func (b *inMsgBuilder) InboundGetStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.builder.clock.Time()
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             GetStateSummaryFrontier,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_GetStateSummaryFrontier{
				GetStateSummaryFrontier: &p2p.GetStateSummaryFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	summary []byte,
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     StateSummaryFrontier,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_StateSummaryFrontier_{
				StateSummaryFrontier_: &p2p.StateSummaryFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Summary:   summary,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundGetAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	heights []uint64,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.builder.clock.Time()
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             GetAcceptedStateSummary,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_GetAcceptedStateSummary{
				GetAcceptedStateSummary: &p2p.GetAcceptedStateSummary{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					Heights:   heights,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	summaryIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	summaryIDBytes := make([][]byte, len(summaryIDs))
	encodeIDs(summaryIDs, summaryIDBytes)
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     AcceptedStateSummary,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_AcceptedStateSummary_{
				AcceptedStateSummary_: &p2p.AcceptedStateSummary{
					ChainId:    chainID[:],
					RequestId:  requestID,
					SummaryIds: summaryIDBytes,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundGetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.builder.clock.Time()
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             GetAcceptedFrontier,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_GetAcceptedFrontier{
				GetAcceptedFrontier: &p2p.GetAcceptedFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     AcceptedFrontier,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_AcceptedFrontier_{
				AcceptedFrontier_: &p2p.AcceptedFrontier{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundGetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.builder.clock.Time()
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             GetAccepted,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_GetAccepted{
				GetAccepted: &p2p.GetAccepted{
					ChainId:      chainID[:],
					RequestId:    requestID,
					Deadline:     uint64(deadline),
					ContainerIds: containerIDBytes,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundAccepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     Accepted,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_Accepted_{
				Accepted_: &p2p.Accepted{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundPushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	container []byte,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.builder.clock.Time()
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             PushQuery,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_PushQuery{
				PushQuery: &p2p.PushQuery{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					Container: container,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundPullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.builder.clock.Time()
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             PullQuery,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_PullQuery{
				PullQuery: &p2p.PullQuery{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundChits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     Chits,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_Chits{
				Chits: &p2p.Chits{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.builder.clock.Time()
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             AppRequest,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_AppRequest{
				AppRequest: &p2p.AppRequest{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					AppBytes:  msg,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundAppResponse(
	chainID ids.ID,
	requestID uint32,
	msg []byte,
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     AppResponse,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_AppResponse{
				AppResponse: &p2p.AppResponse{
					ChainId:   chainID[:],
					RequestId: requestID,
					AppBytes:  msg,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundGet(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	received := b.builder.clock.Time()
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:             Get,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		msg: &p2p.Message{
			Message: &p2p.Message_Get{
				Get: &p2p.Get{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundPut(
	chainID ids.ID,
	requestID uint32,
	container []byte,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     Put,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_Put{
				Put: &p2p.Put{
					ChainId:   chainID[:],
					RequestId: requestID,
					Container: container,
				},
			},
		},
	}
}

func (b *inMsgBuilder) InboundAncestors(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	return &inboundExternalMessage{
		inboundMessage: inboundMessage{
			op:     Ancestors,
			nodeID: nodeID,
		},
		msg: &p2p.Message{
			Message: &p2p.Message_Ancestors_{
				Ancestors_: &p2p.Ancestors{
					ChainId:    chainID[:],
					RequestId:  requestID,
					Containers: containers,
				},
			},
		},
	}
}

func encodeIDs(ids []ids.ID, result [][]byte) {
	for i, id := range ids {
		copy := id
		result[i] = copy[:]
	}
}
