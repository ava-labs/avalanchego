// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ InternalMsgBuilder = internalMsgBuilder{}
	_ InboundMessage     = (*internalMessage)(nil)
)

type InternalMsgBuilder interface {
	InternalFailedRequest(
		op Op,
		nodeID ids.NodeID,
		sourceChainID ids.ID,
		destinationChainID ids.ID,
		requestID uint32,
	) InboundMessage
	InternalCrossChainAppRequest(
		nodeID ids.NodeID,
		sourceChainID ids.ID,
		destinationChainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		msg []byte,
	) InboundMessage
	InternalCrossChainAppResponse(
		nodeID ids.NodeID,
		sourceChainID ids.ID,
		destinationChainID ids.ID,
		requestID uint32,
		msg []byte,
	) InboundMessage
	InternalTimeout(nodeID ids.NodeID) InboundMessage
	InternalConnected(nodeID ids.NodeID, nodeVersion *version.Application) InboundMessage
	InternalDisconnected(nodeID ids.NodeID) InboundMessage
	InternalVMMessage(nodeID ids.NodeID, notification uint32) InboundMessage
	InternalGossipRequest(nodeID ids.NodeID) InboundMessage
}

type internalMsgBuilder struct {
	clock mockable.Clock
}

func NewInternalBuilder() InternalMsgBuilder {
	return internalMsgBuilder{}
}

func (internalMsgBuilder) InternalFailedRequest(
	op Op,
	nodeID ids.NodeID,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &internalMessage{
		inboundMessage: inboundMessage{
			op:     op,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			SourceChainID: sourceChainID[:],
			ChainID:       destinationChainID[:],
			RequestID:     requestID,
		},
	}
}

func (i internalMsgBuilder) InternalCrossChainAppRequest(
	nodeID ids.NodeID,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
) InboundMessage {
	received := i.clock.Time()

	return &internalMessage{
		inboundMessage: inboundMessage{
			op:             CrossChainAppRequest,
			nodeID:         nodeID,
			expirationTime: received.Add(deadline),
		},
		fields: map[Field]interface{}{
			SourceChainID: sourceChainID[:],
			ChainID:       destinationChainID[:],
			RequestID:     requestID,
			AppBytes:      msg,
		},
	}
}

func (internalMsgBuilder) InternalCrossChainAppResponse(
	nodeID ids.NodeID,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	requestID uint32,
	msg []byte,
) InboundMessage {
	return &internalMessage{
		inboundMessage: inboundMessage{
			op:     CrossChainAppResponse,
			nodeID: nodeID,
		},
		fields: map[Field]interface{}{
			SourceChainID: sourceChainID[:],
			ChainID:       destinationChainID[:],
			RequestID:     requestID,
			AppBytes:      msg,
		},
	}
}

func (internalMsgBuilder) InternalTimeout(nodeID ids.NodeID) InboundMessage {
	return &internalMessage{
		inboundMessage: inboundMessage{
			op:     Timeout,
			nodeID: nodeID,
		},
	}
}

func (internalMsgBuilder) InternalConnected(nodeID ids.NodeID, nodeVersion *version.Application) InboundMessage {
	return &internalMessage{
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
	return &internalMessage{
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
	return &internalMessage{
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
	return &internalMessage{
		inboundMessage: inboundMessage{
			op:     GossipRequest,
			nodeID: nodeID,
		},
	}
}

type internalMessage struct {
	inboundMessage

	fields map[Field]interface{}
}

// Field returns the value of the specified field in this message
func (inMsg *internalMessage) Get(field Field) (interface{}, error) {
	value, ok := inMsg.fields[field]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errMissingField, field)
	}
	return value, nil
}

func (inMsg *internalMessage) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("(Op: %s, NodeID: %s", inMsg.op, inMsg.nodeID))
	if requestIDIntf, exists := inMsg.fields[RequestID]; exists {
		sb.WriteString(fmt.Sprintf(", RequestID: %d", requestIDIntf.(uint32)))
	}
	if !inMsg.expirationTime.IsZero() {
		sb.WriteString(fmt.Sprintf(", Deadline: %d", inMsg.expirationTime.Unix()))
	}

	switch inMsg.op {
	case Notify:
		sb.WriteString(fmt.Sprintf(", Notification: %d)", inMsg.fields[VMMessage].(uint32)))
	default:
		sb.WriteString(")")
	}

	return sb.String()
}
