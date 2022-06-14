// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ InboundMsgBuilder = &inMsgBuilder{}

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
		containerID ids.ID,
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

	InboundChitsV2(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		containerID ids.ID,
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
		containerID ids.ID,
		container []byte,
		nodeID ids.NodeID,
	) InboundMessage // used in UTs only
}

type inMsgBuilder struct {
	Codec
	clock mockable.Clock
}

func NewInboundBuilder(c Codec) InboundMsgBuilder {
	return &inMsgBuilder{
		Codec: c,
	}
}

func (b *inMsgBuilder) SetTime(t time.Time) {
	b.clock.Set(t)
	b.Codec.SetTime(t)
}

func (b *inMsgBuilder) InboundGetStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: GetStateSummaryFrontier,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
	}
}

func (b *inMsgBuilder) InboundStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	summary []byte,
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundMessage{
		op: StateSummaryFrontier,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			SummaryBytes: summary,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundGetAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	heights []uint64,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: GetAcceptedStateSummary,
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       uint64(deadline),
			SummaryHeights: heights,
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
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
	return &inboundMessage{
		op: AcceptedStateSummary,
		fields: map[Field]interface{}{
			ChainID:    chainID[:],
			RequestID:  requestID,
			SummaryIDs: summaryIDBytes,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundGetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: GetAcceptedFrontier,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
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
	return &inboundMessage{
		op: AcceptedFrontier,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundGetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerIDs []ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return &inboundMessage{
		op: GetAccepted,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     uint64(deadline),
			ContainerIDs: containerIDBytes,
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
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
	return &inboundMessage{
		op: Accepted,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundPushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	container []byte,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: PushQuery,
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       uint64(deadline),
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
	}
}

func (b *inMsgBuilder) InboundPullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: PullQuery,
		fields: map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    uint64(deadline),
			ContainerID: containerID[:],
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
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
	return &inboundMessage{
		op: Chits,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		nodeID: nodeID,
	}
}

// The first "containerIDs" is always populated for backward compatibilities with old "Chits"
// Only after the DAG is linearized, the second "containerID" will be populated
// with the new snowman chain containers.
func (b *inMsgBuilder) InboundChitsV2(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	containerID ids.ID,
	nodeID ids.NodeID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)

	return &inboundMessage{
		op: ChitsV2,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
			ContainerID:  containerID[:],
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
	nodeID ids.NodeID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: AppRequest,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
			AppBytes:  msg,
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
	}
}

func (b *inMsgBuilder) InboundAppResponse(
	chainID ids.ID,
	requestID uint32,
	msg []byte,
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundMessage{
		op: AppResponse,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			AppBytes:  msg,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundGet(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	received := b.clock.Time()
	return &inboundMessage{
		op: Put,
		fields: map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    uint64(deadline),
			ContainerID: containerID[:],
		},
		nodeID:         nodeID,
		expirationTime: received.Add(deadline),
	}
}

func (b *inMsgBuilder) InboundPut(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	return &inboundMessage{
		op: Put,
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundAncestors(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	nodeID ids.NodeID,
) InboundMessage { // used in UTs only
	return &inboundMessage{
		op: Ancestors,
		fields: map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
		nodeID: nodeID,
	}
}

func encodeIDs(ids []ids.ID, result [][]byte) {
	for i, id := range ids {
		copy := id
		result[i] = copy[:]
	}
}
