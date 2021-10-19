package message

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ InternalMsgBuilder = internalMsgBuilder{}

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

type internalMsgBuilder struct{}

func NewInternalBuilder() InternalMsgBuilder {
	return internalMsgBuilder{}
}

func (internalMsgBuilder) InternalGetAcceptedFrontierFailed(
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
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalGetAcceptedFailed(
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
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalGetAncestorsFailed(
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
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalQueryFailed(
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
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalAppRequestFailed(
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
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalGetFailed(
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
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalTimeout(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:     Timeout,
		fields: make(map[Field]interface{}),
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalConnected(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:     Connected,
		fields: make(map[Field]interface{}),
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalDisconnected(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:     Disconnected,
		nodeID: nodeID,
		fields: make(map[Field]interface{}),
	}
}

func (internalMsgBuilder) InternalVMMessage(
	nodeID ids.ShortID,
	notification uint32,
) InboundMessage {
	return &inboundMessage{
		op: Notify,
		fields: map[Field]interface{}{
			VMMessage: notification,
		},
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalGossipRequest(
	nodeID ids.ShortID,
) InboundMessage {
	return &inboundMessage{
		op:     GossipRequest,
		fields: make(map[Field]interface{}),
		nodeID: nodeID,
	}
}
