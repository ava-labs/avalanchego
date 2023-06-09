// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//nolint:stylecheck // proto generates interfaces that fail linting
package message

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

var (
	disconnected  = &Disconnected{}
	gossipRequest = &GossipRequest{}
	timeout       = &Timeout{}

	_ fmt.Stringer    = (*GetStateSummaryFrontierFailed)(nil)
	_ chainIDGetter   = (*GetStateSummaryFrontierFailed)(nil)
	_ requestIDGetter = (*GetStateSummaryFrontierFailed)(nil)

	_ fmt.Stringer    = (*GetAcceptedStateSummaryFailed)(nil)
	_ chainIDGetter   = (*GetAcceptedStateSummaryFailed)(nil)
	_ requestIDGetter = (*GetAcceptedStateSummaryFailed)(nil)

	_ fmt.Stringer     = (*GetAcceptedFrontierFailed)(nil)
	_ chainIDGetter    = (*GetAcceptedFrontierFailed)(nil)
	_ requestIDGetter  = (*GetAcceptedFrontierFailed)(nil)
	_ engineTypeGetter = (*GetAcceptedFrontierFailed)(nil)

	_ fmt.Stringer     = (*GetAcceptedFailed)(nil)
	_ chainIDGetter    = (*GetAcceptedFailed)(nil)
	_ requestIDGetter  = (*GetAcceptedFailed)(nil)
	_ engineTypeGetter = (*GetAcceptedFailed)(nil)

	_ fmt.Stringer     = (*GetAncestorsFailed)(nil)
	_ chainIDGetter    = (*GetAncestorsFailed)(nil)
	_ requestIDGetter  = (*GetAncestorsFailed)(nil)
	_ engineTypeGetter = (*GetAncestorsFailed)(nil)

	_ fmt.Stringer     = (*GetFailed)(nil)
	_ chainIDGetter    = (*GetFailed)(nil)
	_ requestIDGetter  = (*GetFailed)(nil)
	_ engineTypeGetter = (*GetFailed)(nil)

	_ fmt.Stringer     = (*QueryFailed)(nil)
	_ chainIDGetter    = (*QueryFailed)(nil)
	_ requestIDGetter  = (*QueryFailed)(nil)
	_ engineTypeGetter = (*QueryFailed)(nil)

	_ fmt.Stringer    = (*AppRequestFailed)(nil)
	_ chainIDGetter   = (*AppRequestFailed)(nil)
	_ requestIDGetter = (*AppRequestFailed)(nil)

	_ fmt.Stringer        = (*CrossChainAppRequest)(nil)
	_ sourceChainIDGetter = (*CrossChainAppRequest)(nil)
	_ chainIDGetter       = (*CrossChainAppRequest)(nil)
	_ requestIDGetter     = (*CrossChainAppRequest)(nil)

	_ fmt.Stringer        = (*CrossChainAppRequestFailed)(nil)
	_ sourceChainIDGetter = (*CrossChainAppRequestFailed)(nil)
	_ chainIDGetter       = (*CrossChainAppRequestFailed)(nil)
	_ requestIDGetter     = (*CrossChainAppRequestFailed)(nil)

	_ fmt.Stringer        = (*CrossChainAppResponse)(nil)
	_ sourceChainIDGetter = (*CrossChainAppResponse)(nil)
	_ chainIDGetter       = (*CrossChainAppResponse)(nil)
	_ requestIDGetter     = (*CrossChainAppResponse)(nil)

	_ fmt.Stringer = (*Disconnected)(nil)

	_ fmt.Stringer = (*GossipRequest)(nil)

	_ fmt.Stringer = (*Timeout)(nil)
)

type GetStateSummaryFrontierFailed struct {
	ChainID   ids.ID `json:"chain_id,omitempty"`
	RequestID uint32 `json:"request_id,omitempty"`
}

func (m *GetStateSummaryFrontierFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d",
		m.ChainID, m.RequestID,
	)
}

func (m *GetStateSummaryFrontierFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetStateSummaryFrontierFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalGetStateSummaryFrontierFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetStateSummaryFrontierFailedOp,
		message: &GetStateSummaryFrontierFailed{
			ChainID:   chainID,
			RequestID: requestID,
		},
		expiration: mockable.MaxTime,
	}
}

type GetAcceptedStateSummaryFailed struct {
	ChainID   ids.ID `json:"chain_id,omitempty"`
	RequestID uint32 `json:"request_id,omitempty"`
}

func (m *GetAcceptedStateSummaryFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d",
		m.ChainID, m.RequestID,
	)
}

func (m *GetAcceptedStateSummaryFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetAcceptedStateSummaryFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalGetAcceptedStateSummaryFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetAcceptedStateSummaryFailedOp,
		message: &GetAcceptedStateSummaryFailed{
			ChainID:   chainID,
			RequestID: requestID,
		},
		expiration: mockable.MaxTime,
	}
}

type GetAcceptedFrontierFailed struct {
	ChainID    ids.ID         `json:"chain_id,omitempty"`
	RequestID  uint32         `json:"request_id,omitempty"`
	EngineType p2p.EngineType `json:"engine_type,omitempty"`
}

func (m *GetAcceptedFrontierFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d EngineType: %s",
		m.ChainID, m.RequestID, m.EngineType,
	)
}

func (m *GetAcceptedFrontierFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetAcceptedFrontierFailed) GetRequestId() uint32 {
	return m.RequestID
}

func (m *GetAcceptedFrontierFailed) GetEngineType() p2p.EngineType {
	return m.EngineType
}

func InternalGetAcceptedFrontierFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	engineType p2p.EngineType,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetAcceptedFrontierFailedOp,
		message: &GetAcceptedFrontierFailed{
			ChainID:    chainID,
			RequestID:  requestID,
			EngineType: engineType,
		},
		expiration: mockable.MaxTime,
	}
}

type GetAcceptedFailed struct {
	ChainID    ids.ID         `json:"chain_id,omitempty"`
	RequestID  uint32         `json:"request_id,omitempty"`
	EngineType p2p.EngineType `json:"engine_type,omitempty"`
}

func (m *GetAcceptedFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d EngineType: %s",
		m.ChainID, m.RequestID, m.EngineType,
	)
}

func (m *GetAcceptedFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetAcceptedFailed) GetRequestId() uint32 {
	return m.RequestID
}

func (m *GetAcceptedFailed) GetEngineType() p2p.EngineType {
	return m.EngineType
}

func InternalGetAcceptedFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	engineType p2p.EngineType,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetAcceptedFailedOp,
		message: &GetAcceptedFailed{
			ChainID:    chainID,
			RequestID:  requestID,
			EngineType: engineType,
		},
		expiration: mockable.MaxTime,
	}
}

type GetAncestorsFailed struct {
	ChainID    ids.ID         `json:"chain_id,omitempty"`
	RequestID  uint32         `json:"request_id,omitempty"`
	EngineType p2p.EngineType `json:"engine_type,omitempty"`
}

func (m *GetAncestorsFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d EngineType: %s",
		m.ChainID, m.RequestID, m.EngineType,
	)
}

func (m *GetAncestorsFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetAncestorsFailed) GetRequestId() uint32 {
	return m.RequestID
}

func (m *GetAncestorsFailed) GetEngineType() p2p.EngineType {
	return m.EngineType
}

func InternalGetAncestorsFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	engineType p2p.EngineType,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetAncestorsFailedOp,
		message: &GetAncestorsFailed{
			ChainID:    chainID,
			RequestID:  requestID,
			EngineType: engineType,
		},
		expiration: mockable.MaxTime,
	}
}

type GetFailed struct {
	ChainID    ids.ID         `json:"chain_id,omitempty"`
	RequestID  uint32         `json:"request_id,omitempty"`
	EngineType p2p.EngineType `json:"engine_type,omitempty"`
}

func (m *GetFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d EngineType: %s",
		m.ChainID, m.RequestID, m.EngineType,
	)
}

func (m *GetFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetFailed) GetRequestId() uint32 {
	return m.RequestID
}

func (m *GetFailed) GetEngineType() p2p.EngineType {
	return m.EngineType
}

func InternalGetFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	engineType p2p.EngineType,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetFailedOp,
		message: &GetFailed{
			ChainID:    chainID,
			RequestID:  requestID,
			EngineType: engineType,
		},
		expiration: mockable.MaxTime,
	}
}

type QueryFailed struct {
	ChainID    ids.ID         `json:"chain_id,omitempty"`
	RequestID  uint32         `json:"request_id,omitempty"`
	EngineType p2p.EngineType `json:"engine_type,omitempty"`
}

func (m *QueryFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d EngineType: %s",
		m.ChainID, m.RequestID, m.EngineType,
	)
}

func (m *QueryFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *QueryFailed) GetRequestId() uint32 {
	return m.RequestID
}

func (m *QueryFailed) GetEngineType() p2p.EngineType {
	return m.EngineType
}

func InternalQueryFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	engineType p2p.EngineType,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     QueryFailedOp,
		message: &QueryFailed{
			ChainID:    chainID,
			RequestID:  requestID,
			EngineType: engineType,
		},
		expiration: mockable.MaxTime,
	}
}

type AppRequestFailed struct {
	ChainID   ids.ID `json:"chain_id,omitempty"`
	RequestID uint32 `json:"request_id,omitempty"`
}

func (m *AppRequestFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d",
		m.ChainID, m.RequestID,
	)
}

func (m *AppRequestFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *AppRequestFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalAppRequestFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     AppRequestFailedOp,
		message: &AppRequestFailed{
			ChainID:   chainID,
			RequestID: requestID,
		},
		expiration: mockable.MaxTime,
	}
}

type CrossChainAppRequest struct {
	SourceChainID      ids.ID `json:"source_chain_id,omitempty"`
	DestinationChainID ids.ID `json:"destination_chain_id,omitempty"`
	RequestID          uint32 `json:"request_id,omitempty"`
	Message            []byte `json:"message,omitempty"`
}

func (m *CrossChainAppRequest) String() string {
	return fmt.Sprintf(
		"SourceChainID: %s DestinationChainID: %s RequestID: %d Message: 0x%x",
		m.SourceChainID, m.DestinationChainID, m.RequestID, m.Message,
	)
}

func (m *CrossChainAppRequest) GetSourceChainID() ids.ID {
	return m.SourceChainID
}

func (m *CrossChainAppRequest) GetChainId() []byte {
	return m.DestinationChainID[:]
}

func (m *CrossChainAppRequest) GetRequestId() uint32 {
	return m.RequestID
}

func InternalCrossChainAppRequest(
	nodeID ids.NodeID,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     CrossChainAppRequestOp,
		message: &CrossChainAppRequest{
			SourceChainID:      sourceChainID,
			DestinationChainID: destinationChainID,
			RequestID:          requestID,
			Message:            msg,
		},
		expiration: time.Now().Add(deadline),
	}
}

type CrossChainAppRequestFailed struct {
	SourceChainID      ids.ID `json:"source_chain_id,omitempty"`
	DestinationChainID ids.ID `json:"destination_chain_id,omitempty"`
	RequestID          uint32 `json:"request_id,omitempty"`
}

func (m *CrossChainAppRequestFailed) String() string {
	return fmt.Sprintf(
		"SourceChainID: %s DestinationChainID: %s RequestID: %d",
		m.SourceChainID, m.DestinationChainID, m.RequestID,
	)
}

func (m *CrossChainAppRequestFailed) GetSourceChainID() ids.ID {
	return m.SourceChainID
}

func (m *CrossChainAppRequestFailed) GetChainId() []byte {
	return m.DestinationChainID[:]
}

func (m *CrossChainAppRequestFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalCrossChainAppRequestFailed(
	nodeID ids.NodeID,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     CrossChainAppRequestFailedOp,
		message: &CrossChainAppRequestFailed{
			SourceChainID:      sourceChainID,
			DestinationChainID: destinationChainID,
			RequestID:          requestID,
		},
		expiration: mockable.MaxTime,
	}
}

type CrossChainAppResponse struct {
	SourceChainID      ids.ID `json:"source_chain_id,omitempty"`
	DestinationChainID ids.ID `json:"destination_chain_id,omitempty"`
	RequestID          uint32 `json:"request_id,omitempty"`
	Message            []byte `json:"message,omitempty"`
}

func (m *CrossChainAppResponse) String() string {
	return fmt.Sprintf(
		"SourceChainID: %s DestinationChainID: %s RequestID: %d Message: 0x%x",
		m.SourceChainID, m.DestinationChainID, m.RequestID, m.Message,
	)
}

func (m *CrossChainAppResponse) GetSourceChainID() ids.ID {
	return m.SourceChainID
}

func (m *CrossChainAppResponse) GetChainId() []byte {
	return m.DestinationChainID[:]
}

func (m *CrossChainAppResponse) GetRequestId() uint32 {
	return m.RequestID
}

func InternalCrossChainAppResponse(
	nodeID ids.NodeID,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	requestID uint32,
	msg []byte,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     CrossChainAppResponseOp,
		message: &CrossChainAppResponse{
			SourceChainID:      sourceChainID,
			DestinationChainID: destinationChainID,
			RequestID:          requestID,
			Message:            msg,
		},
		expiration: mockable.MaxTime,
	}
}

type Connected struct {
	NodeVersion *version.Application `json:"node_version,omitempty"`
}

func (m *Connected) String() string {
	return fmt.Sprintf(
		"NodeVersion: %s",
		m.NodeVersion,
	)
}

func InternalConnected(nodeID ids.NodeID, nodeVersion *version.Application) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     ConnectedOp,
		message: &Connected{
			NodeVersion: nodeVersion,
		},
		expiration: mockable.MaxTime,
	}
}

// ConnectedSubnet contains the subnet ID of the subnet that the node is
// connected to.
type ConnectedSubnet struct {
	SubnetID ids.ID `json:"subnet_id,omitempty"`
}

func (m *ConnectedSubnet) String() string {
	return fmt.Sprintf(
		"SubnetID: %s",
		m.SubnetID,
	)
}

// InternalConnectedSubnet returns a message that indicates the node with [nodeID] is
// connected to the subnet with the given [subnetID].
func InternalConnectedSubnet(nodeID ids.NodeID, subnetID ids.ID) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     ConnectedSubnetOp,
		message: &ConnectedSubnet{
			SubnetID: subnetID,
		},
		expiration: mockable.MaxTime,
	}
}

type Disconnected struct{}

func (Disconnected) String() string {
	return ""
}

func InternalDisconnected(nodeID ids.NodeID) InboundMessage {
	return &inboundMessage{
		nodeID:     nodeID,
		op:         DisconnectedOp,
		message:    disconnected,
		expiration: mockable.MaxTime,
	}
}

type VMMessage struct {
	Notification uint32 `json:"notification,omitempty"`
}

func (m *VMMessage) String() string {
	return fmt.Sprintf(
		"Notification: %d",
		m.Notification,
	)
}

func InternalVMMessage(
	nodeID ids.NodeID,
	notification uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     NotifyOp,
		message: &VMMessage{
			Notification: notification,
		},
		expiration: mockable.MaxTime,
	}
}

type GossipRequest struct{}

func (GossipRequest) String() string {
	return ""
}

func InternalGossipRequest(
	nodeID ids.NodeID,
) InboundMessage {
	return &inboundMessage{
		nodeID:     nodeID,
		op:         GossipRequestOp,
		message:    gossipRequest,
		expiration: mockable.MaxTime,
	}
}

type Timeout struct{}

func (Timeout) String() string {
	return ""
}

func InternalTimeout(nodeID ids.NodeID) InboundMessage {
	return &inboundMessage{
		nodeID:     nodeID,
		op:         TimeoutOp,
		message:    timeout,
		expiration: mockable.MaxTime,
	}
}
