// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ InboundMsgBuilder = &inMsgBuilderWithPacker{}

type InboundMsgBuilder interface {
	Parser

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

type inMsgBuilderWithPacker struct {
	Codec
	clock mockable.Clock
}

func NewInboundBuilderWithPacker(c Codec) InboundMsgBuilder {
	return &inMsgBuilderWithPacker{
		Codec: c,
	}
}

func (b *inMsgBuilderWithPacker) SetTime(t time.Time) {
	b.clock.Set(t)
	b.Codec.SetTime(t)
}

func (b *inMsgBuilderWithPacker) InboundGetStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             GetStateSummaryFrontier,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	summary []byte,
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     StateSummaryFrontier,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			SummaryBytes: summary,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundGetAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	heights []uint64,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             GetAcceptedStateSummary,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       uint64(deadline),
			SummaryHeights: heights,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	summaryIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	summaryIDBytes := make([][]byte, len(summaryIDs))
	encodeIDs(summaryIDs, summaryIDBytes)
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     AcceptedStateSummary,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:    chainID[:],
			RequestID:  requestID,
			SummaryIDs: summaryIDBytes,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundGetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             GetAcceptedFrontier,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     AcceptedFrontier,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundGetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             GetAccepted,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     uint64(deadline),
			ContainerIDs: containerIDBytes,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundAccepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Accepted,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundPushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	container []byte,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             PushQuery,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       uint64(deadline),
			ContainerBytes: container,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundPullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             PullQuery,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    uint64(deadline),
			ContainerID: containerID[:],
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundChits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Chits,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             AppRequest,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
			AppBytes:  msg,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundAppResponse(
	chainID ids.ID,
	requestID uint32,
	msg []byte,
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     AppResponse,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			AppBytes:  msg,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundGet(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	received := b.clock.Time()
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:             Put,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    uint64(deadline),
			ContainerID: containerID[:],
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundPut(
	chainID ids.ID,
	requestID uint32,
	container []byte,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Put,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			ContainerBytes: container,
		},
	}
}

func (b *inMsgBuilderWithPacker) InboundAncestors(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Ancestors,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
	}
}

func encodeIDs(ids []ids.ID, result [][]byte) {
	for i, id := range ids {
		copy := id
		result[i] = copy[:]
	}
}
