package message

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ InternalMsgBuilder = internalMsgBuilder{}

type InternalMsgBuilder interface {
	InternalFailedRequest(
		op Op,
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

func (internalMsgBuilder) InternalFailedRequest(
	op Op,
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		op: op,
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
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalConnected(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:     Connected,
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalDisconnected(nodeID ids.ShortID) InboundMessage {
	return &inboundMessage{
		op:     Disconnected,
		nodeID: nodeID,
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
		nodeID: nodeID,
	}
}
