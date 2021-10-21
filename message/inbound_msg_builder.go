package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ InboundMsgBuilder = &inMsgBuilder{}

type InboundMsgBuilder interface {
	SetTime(t time.Time) // useful in UTs

	Parse(
		bytes []byte,
		nodeID ids.ShortID,
		onFinishedHandling func(),
	) (InboundMessage, error)

	InboundGetAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
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
		deadline uint64,
		containerIDs []ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundAccepted(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		nodeID ids.ShortID,
	) InboundMessage

	InboundMultiPut(
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
		nodeID ids.ShortID,
	) InboundMessage // used in UTs only

	InboundPushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		containerID ids.ID,
		container []byte,
		nodeID ids.ShortID,
	) InboundMessage

	InboundPullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
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
		deadline uint64,
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
		deadline uint64,
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
	c     Codec
	clock mockable.Clock
}

func NewInboundBuilder(c Codec) InboundMsgBuilder {
	return &inMsgBuilder{
		c: c,
	}
}

func (b *inMsgBuilder) SetTime(t time.Time) {
	b.clock.Set(t)
}

func (b *inMsgBuilder) InboundGetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	nodeID ids.ShortID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: GetAcceptedFrontier,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  deadline,
		},
		nodeID:         nodeID,
		expirationTime: received.Add(time.Duration(deadline)),
	}
}

func (b *inMsgBuilder) InboundAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.ShortID,
) InboundMessage {
	return &inboundMessage{
		op: AcceptedFrontier,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundGetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerIDs []ids.ID,
	nodeID ids.ShortID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: GetAccepted,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     deadline,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
		nodeID:         nodeID,
		expirationTime: received.Add(time.Duration(deadline)),
	}
}

func (b *inMsgBuilder) InboundAccepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.ShortID,
) InboundMessage {
	return &inboundMessage{
		op: Accepted,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundPushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
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
			Deadline:       deadline,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		nodeID:         nodeID,
		expirationTime: received.Add(time.Duration(deadline)),
	}
}

func (b *inMsgBuilder) InboundPullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	nodeID ids.ShortID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: PullQuery,
		fields: map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		nodeID:         nodeID,
		expirationTime: received.Add(time.Duration(deadline)),
	}
}

func (b *inMsgBuilder) InboundChits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	nodeID ids.ShortID,
) InboundMessage {
	return &inboundMessage{
		op: Chits,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	msg []byte,
	nodeID ids.ShortID,
) InboundMessage {
	received := b.clock.Time()
	return &inboundMessage{
		op: AppRequest,
		fields: map[Field]interface{}{
			ChainID:         chainID[:],
			RequestID:       requestID,
			Deadline:        deadline,
			AppRequestBytes: AppRequestBytes,
		},
		nodeID:         nodeID,
		expirationTime: received.Add(time.Duration(deadline)),
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
			ChainID:          chainID[:],
			RequestID:        requestID,
			AppResponseBytes: msg,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) InboundGet(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	nodeID ids.ShortID,
) InboundMessage { // used in UTs only
	received := b.clock.Time()
	return &inboundMessage{
		op: Put,
		fields: map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		nodeID:         nodeID,
		expirationTime: received.Add(time.Duration(deadline)),
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

func (b *inMsgBuilder) InboundMultiPut(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	nodeID ids.ShortID,
) InboundMessage { // used in UTs only
	return &inboundMessage{
		op: MultiPut,
		fields: map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
		nodeID: nodeID,
	}
}

func (b *inMsgBuilder) Parse(bytes []byte, nodeID ids.ShortID, onFinishedHandling func()) (InboundMessage, error) {
	return b.c.Parse(bytes, nodeID, onFinishedHandling)
}
