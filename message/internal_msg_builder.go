// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	return &inboundMessage{
		op: op,
		fields: map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
		},
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalTimeout(nodeID ids.NodeID) InboundMessage {
	return &inboundMessage{
		op:     Timeout,
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalConnected(nodeID ids.NodeID, nodeVersion *version.Application) InboundMessage {
	return &inboundMessage{
		op: Connected,
		fields: map[Field]interface{}{
			VersionStruct: nodeVersion,
		},
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalDisconnected(nodeID ids.NodeID) InboundMessage {
	return &inboundMessage{
		op:     Disconnected,
		nodeID: nodeID,
	}
}

func (internalMsgBuilder) InternalVMMessage(
	nodeID ids.NodeID,
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
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundMessage{
		op:     GossipRequest,
		nodeID: nodeID,
	}
}
