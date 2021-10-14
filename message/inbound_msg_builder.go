package message

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ InboundMsgBuilder = &inMsgBuilder{}

type InboundMsgBuilder interface {
	InboundGetAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
	) InboundMessage

	InboundAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) InboundMessage

	InboundGetAccepted(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		containerIDs []ids.ID,
	) InboundMessage

	InboundAccepted(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) InboundMessage

	InboundPushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		containerID ids.ID,
		container []byte,
	) InboundMessage

	InboundPullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		containerID ids.ID,
	) InboundMessage

	InboundChits(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) InboundMessage

	InboundAppRequest(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		msg []byte,
	) InboundMessage

	InboundAppResponse(
		chainID ids.ID,
		requestID uint32,
		msg []byte,
	) InboundMessage

	InboundPut(
		chainID ids.ID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
	) InboundMessage // used in UTs only

	InboundMultiPut(
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
	) InboundMessage // used in UTs only

	Parse(bytes []byte) (InboundMessage, error)
}

type inMsgBuilder struct {
	c Codec
}

func NewInboundBuilder(c Codec) InboundMsgBuilder {
	return &inMsgBuilder{
		c: c,
	}
}

func (b *inMsgBuilder) InboundGetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
) InboundMessage {
	return &inboundMessage{
		op: GetAcceptedFrontier,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  deadline,
		},
	}
}

func (b *inMsgBuilder) InboundAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) InboundMessage {
	return &inboundMessage{
		op: AcceptedFrontier,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
	}
}

func (b *inMsgBuilder) InboundGetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerIDs []ids.ID,
) InboundMessage {
	return &inboundMessage{
		op: GetAccepted,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     deadline,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
	}
}

func (b *inMsgBuilder) InboundAccepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) InboundMessage {
	return &inboundMessage{
		op: Accepted,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
	}
}

func (b *inMsgBuilder) InboundPushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	container []byte,
) InboundMessage {
	return &inboundMessage{
		op: PushQuery,
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       deadline,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
	}
}

func (b *inMsgBuilder) InboundPullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
) InboundMessage {
	return &inboundMessage{
		op: PullQuery,
		fields: map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
	}
}

func (b *inMsgBuilder) InboundChits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) InboundMessage {
	return &inboundMessage{
		op: Chits,
		fields: map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: encodeContainerIDs(containerIDs),
		},
	}
}

func (b *inMsgBuilder) InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	msg []byte,
) InboundMessage {
	return &inboundMessage{
		op: AppRequest,
		fields: map[Field]interface{}{
			ChainID:         chainID[:],
			RequestID:       requestID,
			Deadline:        deadline,
			AppRequestBytes: AppRequestBytes,
		},
	}
}

func (b *inMsgBuilder) InboundAppResponse(
	chainID ids.ID,
	requestID uint32,
	msg []byte,
) InboundMessage {
	return &inboundMessage{
		op: AppResponse,
		fields: map[Field]interface{}{
			ChainID:          chainID[:],
			RequestID:        requestID,
			AppResponseBytes: msg,
		},
	}
}

func (b *inMsgBuilder) InboundPut(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
) InboundMessage { // used in UTs only
	return &inboundMessage{
		op: Put,
		fields: map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
	}
}

func (b *inMsgBuilder) InboundMultiPut(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
) InboundMessage { // used in UTs only
	return &inboundMessage{
		op: MultiPut,
		fields: map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
	}
}

func (b *inMsgBuilder) Parse(bytes []byte) (InboundMessage, error) {
	return b.c.Parse(bytes)
}
