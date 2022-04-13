// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/version"
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
	InternalConnected(nodeID ids.ShortID, nodeVersion version.Application) InboundMessage
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

func (internalMsgBuilder) InternalConnected(nodeID ids.ShortID, nodeVersion version.Application) InboundMessage {
	return &inboundMessage{
		op: Connected,
		fields: map[Field]interface{}{
			VersionStruct: nodeVersion,
		},
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
