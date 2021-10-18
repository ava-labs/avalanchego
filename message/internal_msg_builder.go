package message

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ InternalMsgBuilder = &internalMsgBuilder{}

type InternalMsgBuilder interface {
	InternalGetAcceptedFrontierFailed(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
	) InboundMessage

	InternalGetAcceptedFailed(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
	) InboundMessage

	InternalGetAncestorsFailed(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
	) InboundMessage

	InternalQueryFailed(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
	) InboundMessage

	InternalAppRequestFailed(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
	) InboundMessage

	InternalGetFailed(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
	) InboundMessage

	InternalTimeout(nodeID ids.ShortID) InboundMessage
	InternalConnected(nodeID ids.ShortID) InboundMessage
	InternalDisconnected(nodeID ids.ShortID) InboundMessage
	InternalVMMessage(nodeID ids.ShortID, notification uint32) InboundMessage
	InternalGossipRequest(nodeID ids.ShortID) InboundMessage
}

type internalMsgBuilder struct {
	c Codec
}

func NewInternalBuilder(c Codec) InternalMsgBuilder {
	return &internalMsgBuilder{
		c: c,
	}
}

func (b *internalMsgBuilder) InternalGetAcceptedFrontierFailed(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		op: GetAcceptedFrontierFailed,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalGetAcceptedFailed(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		op: GetAcceptedFailed,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalGetAncestorsFailed(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		op: GetAncestorsFailed,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalQueryFailed(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		op: QueryFailed,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalAppRequestFailed(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		op: AppRequestFailed,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalGetFailed(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		op: GetFailed,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalTimeout(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:                 Timeout,
		fields:             make(map[Field]interface{}),
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalConnected(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:                 Connected,
		fields:             make(map[Field]interface{}),
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalDisconnected(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:                 Disconnected,
		nodeID:             nodeID,
		fields:             make(map[Field]interface{}),
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalVMMessage(
	nodeID ids.ShortID,
	notification uint32,
) InboundMessage {
	return &inboundMessage{
		op: Notify,
		fields: map[Field]interface{}{
			VMMessage: notification,
		},
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}

func (b *internalMsgBuilder) InternalGossipRequest(
	nodeID ids.ShortID,
) InboundMessage {
	return &inboundMessage{
		op:                 GossipRequest,
		fields:             make(map[Field]interface{}),
		nodeID:             nodeID,
		onFinishedHandling: func() {},
	}
}
