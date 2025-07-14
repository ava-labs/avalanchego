// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//nolint:staticcheck // proto generates interfaces that fail linting
package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/buf/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

var (
	disconnected  = &Disconnected{}
	gossipRequest = &GossipRequest{}

	_ fmt.Stringer    = (*GetStateSummaryFrontierFailed)(nil)
	_ chainIDGetter   = (*GetStateSummaryFrontierFailed)(nil)
	_ requestIDGetter = (*GetStateSummaryFrontierFailed)(nil)

	_ fmt.Stringer    = (*GetAcceptedStateSummaryFailed)(nil)
	_ chainIDGetter   = (*GetAcceptedStateSummaryFailed)(nil)
	_ requestIDGetter = (*GetAcceptedStateSummaryFailed)(nil)

	_ fmt.Stringer    = (*GetAcceptedFrontierFailed)(nil)
	_ chainIDGetter   = (*GetAcceptedFrontierFailed)(nil)
	_ requestIDGetter = (*GetAcceptedFrontierFailed)(nil)

	_ fmt.Stringer    = (*GetAcceptedFailed)(nil)
	_ chainIDGetter   = (*GetAcceptedFailed)(nil)
	_ requestIDGetter = (*GetAcceptedFailed)(nil)

	_ fmt.Stringer     = (*GetAncestorsFailed)(nil)
	_ chainIDGetter    = (*GetAncestorsFailed)(nil)
	_ requestIDGetter  = (*GetAncestorsFailed)(nil)
	_ engineTypeGetter = (*GetAncestorsFailed)(nil)

	_ fmt.Stringer    = (*GetFailed)(nil)
	_ chainIDGetter   = (*GetFailed)(nil)
	_ requestIDGetter = (*GetFailed)(nil)

	_ fmt.Stringer    = (*QueryFailed)(nil)
	_ chainIDGetter   = (*QueryFailed)(nil)
	_ requestIDGetter = (*QueryFailed)(nil)

	_ fmt.Stringer = (*Disconnected)(nil)

	_ fmt.Stringer = (*GossipRequest)(nil)
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
	ChainID   ids.ID `json:"chain_id,omitempty"`
	RequestID uint32 `json:"request_id,omitempty"`
}

func (m *GetAcceptedFrontierFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d",
		m.ChainID, m.RequestID,
	)
}

func (m *GetAcceptedFrontierFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetAcceptedFrontierFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalGetAcceptedFrontierFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetAcceptedFrontierFailedOp,
		message: &GetAcceptedFrontierFailed{
			ChainID:   chainID,
			RequestID: requestID,
		},
		expiration: mockable.MaxTime,
	}
}

type GetAcceptedFailed struct {
	ChainID   ids.ID `json:"chain_id,omitempty"`
	RequestID uint32 `json:"request_id,omitempty"`
}

func (m *GetAcceptedFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d",
		m.ChainID, m.RequestID,
	)
}

func (m *GetAcceptedFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetAcceptedFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalGetAcceptedFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetAcceptedFailedOp,
		message: &GetAcceptedFailed{
			ChainID:   chainID,
			RequestID: requestID,
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
	ChainID   ids.ID `json:"chain_id,omitempty"`
	RequestID uint32 `json:"request_id,omitempty"`
}

func (m *GetFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d",
		m.ChainID, m.RequestID,
	)
}

func (m *GetFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *GetFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalGetFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     GetFailedOp,
		message: &GetFailed{
			ChainID:   chainID,
			RequestID: requestID,
		},
		expiration: mockable.MaxTime,
	}
}

type QueryFailed struct {
	ChainID   ids.ID `json:"chain_id,omitempty"`
	RequestID uint32 `json:"request_id,omitempty"`
}

func (m *QueryFailed) String() string {
	return fmt.Sprintf(
		"ChainID: %s RequestID: %d",
		m.ChainID, m.RequestID,
	)
}

func (m *QueryFailed) GetChainId() []byte {
	return m.ChainID[:]
}

func (m *QueryFailed) GetRequestId() uint32 {
	return m.RequestID
}

func InternalQueryFailed(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) InboundMessage {
	return &inboundMessage{
		nodeID: nodeID,
		op:     QueryFailedOp,
		message: &QueryFailed{
			ChainID:   chainID,
			RequestID: requestID,
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
