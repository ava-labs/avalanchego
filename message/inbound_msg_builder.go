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

	InboundGetAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		nodeID ids.ShortID,
	) InboundMessage

	InboundAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundGetAccepted(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerIDs []ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundAccepted(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundAncestors(
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
		nodeID ids.ShortID,
	) InboundMessage // used in UTs only

	InboundPushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
		container []byte,
		nodeID ids.ShortID,
	) InboundMessage

	InboundPullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundChits(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundAppRequest(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		msg []byte,
		nodeID ids.ShortID,
	) InboundMessage

	InboundAppResponse(
		chainID ids.ID,
		requestID uint32,
		msg []byte,
		nodeID ids.ShortID,
	) InboundMessage

	InboundGet(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundPut(
		chainID ids.ID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
		nodeID ids.ShortID,
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

func (b *inMsgBuilder) InboundGetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	nodeID ids.ShortID,
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
	nodeID ids.ShortID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeContainerIDs(containerIDs, containerIDBytes)
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
	nodeID ids.ShortID,
) InboundMessage {
	received := b.clock.Time()
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeContainerIDs(containerIDs, containerIDBytes)
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
	nodeID ids.ShortID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeContainerIDs(containerIDs, containerIDBytes)
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
	nodeID ids.ShortID,
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
	nodeID ids.ShortID,
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
	nodeID ids.ShortID,
) InboundMessage {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeContainerIDs(containerIDs, containerIDBytes)
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

func (b *inMsgBuilder) InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
	nodeID ids.ShortID,
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
	nodeID ids.ShortID,
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
	nodeID ids.ShortID,
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
	nodeID ids.ShortID,
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
	nodeID ids.ShortID,
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

func encodeContainerIDs(containerIDs []ids.ID, result [][]byte) {
	for i, containerID := range containerIDs {
		copy := containerID
		result[i] = copy[:]
	}
}
