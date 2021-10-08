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
	) (InboundMessage, error)

	InboundAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) (InboundMessage, error)

	InboundGetAccepted(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		containerIDs []ids.ID,
	) (InboundMessage, error)

	InboundAccepted(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) (InboundMessage, error)

	InboundPushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		containerID ids.ID,
		container []byte,
	) (InboundMessage, error)

	InboundPullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		containerID ids.ID,
	) (InboundMessage, error)

	InboundChits(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) (InboundMessage, error)

	InboundAppRequest(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
		msg []byte,
	) (InboundMessage, error)

	InboundAppResponse(
		chainID ids.ID,
		requestID uint32,
		msg []byte,
	) (InboundMessage, error)

	InboundPut(
		chainID ids.ID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
	) (InboundMessage, error) // used in UTs only

	InboundMultiPut(
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
	) (InboundMessage, error) // used in UTs only
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
) (InboundMessage, error) {
	inOp := GetAcceptedFrontier

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[Deadline] = deadline

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (InboundMessage, error) {
	inOp := AcceptedFrontier

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[ContainerIDs] = encodeContainerIDs(containerIDs)

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundGetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerIDs []ids.ID,
) (InboundMessage, error) {
	inOp := GetAccepted

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[Deadline] = deadline
	fieldValues[ContainerIDs] = encodeContainerIDs(containerIDs)

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundAccepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (InboundMessage, error) {
	inOp := Accepted

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[ContainerIDs] = encodeContainerIDs(containerIDs)

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundPushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	container []byte,
) (InboundMessage, error) {
	inOp := PushQuery

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[Deadline] = deadline
	fieldValues[ContainerID] = containerID
	fieldValues[ContainerBytes] = container

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundPullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
) (InboundMessage, error) {
	inOp := PullQuery

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[Deadline] = deadline
	fieldValues[ContainerID] = containerID

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundChits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (InboundMessage, error) {
	inOp := Chits

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[ContainerIDs] = encodeContainerIDs(containerIDs)

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundAppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	msg []byte,
) (InboundMessage, error) {
	inOp := AppRequest

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[Deadline] = deadline
	fieldValues[AppRequestBytes] = AppRequestBytes

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundAppResponse(
	chainID ids.ID,
	requestID uint32,
	msg []byte,
) (InboundMessage, error) {
	inOp := AppResponse

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[AppResponseBytes] = msg

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundPut(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
) (InboundMessage, error) { // used in UTs only
	inOp := Put

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[ContainerID] = containerID[:]
	fieldValues[ContainerBytes] = container

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}

func (b *inMsgBuilder) InboundMultiPut(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
) (InboundMessage, error) { // used in UTs only
	inOp := MultiPut

	fieldValues := make(map[Field]interface{})
	fieldValues[ChainID] = chainID[:]
	fieldValues[RequestID] = requestID
	fieldValues[MultiContainerBytes] = containers

	res := &inboundMessage{
		op:                    inOp,
		bytesSavedCompression: 0,
		fields:                fieldValues,
	}
	return res, nil
}
