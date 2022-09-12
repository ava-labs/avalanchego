// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
)

var _ InternalMsgBuilder = internalMsgBuilder{}

type InternalMsgBuilder interface {
	InternalFailedRequest(
		op Op,
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID uint32,
	) InboundMessage

	InternalTimeout(nodeID ids.NodeID) InboundMessage
	InternalConnected(nodeID ids.NodeID, nodeVersion *version.Application) InboundMessage
	InternalDisconnected(nodeID ids.NodeID) InboundMessage
	InternalVMMessage(nodeID ids.NodeID, notification uint32) InboundMessage
	InternalGossipRequest(nodeID ids.NodeID) InboundMessage
}

type internalMsgBuilder struct{}

func NewInternalBuilder() InternalMsgBuilder {
	return internalMsgBuilder{}
}

func (internalMsgBuilder) InternalFailedRequest(
	op Op,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     op,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
	}
}

func (internalMsgBuilder) InternalTimeout(nodeID ids.NodeID) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Timeout,
			nodeID: nodeID,
		},
	}
}

func (internalMsgBuilder) InternalConnected(nodeID ids.NodeID, nodeVersion *version.Application) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Connected,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			VersionStruct: nodeVersion,
		},
	}
}

func (internalMsgBuilder) InternalDisconnected(nodeID ids.NodeID) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Disconnected,
			nodeID: nodeID,
		},
	}
}

func (internalMsgBuilder) InternalVMMessage(
	nodeID ids.NodeID,
	notification uint32,
) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     Notify,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			VMMessage: notification,
		},
	}
}

func (internalMsgBuilder) InternalGossipRequest(
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op:     GossipRequest,
			nodeID: nodeID,
		},
	}
}
