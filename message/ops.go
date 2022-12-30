// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Op is an opcode
type Op byte

// Types of messages that may be sent between nodes
// Note: If you add a new parseable Op below, you must also add it to ops
// (declared below)
const (
	// Handshake:
	PingOp Op = iota
	PongOp
	VersionOp
	PeerListOp
	PeerListAckOp
	// State sync:
	GetStateSummaryFrontierOp
	GetStateSummaryFrontierFailedOp
	StateSummaryFrontierOp
	GetAcceptedStateSummaryOp
	GetAcceptedStateSummaryFailedOp
	AcceptedStateSummaryOp
	// Bootstrapping:
	GetAcceptedFrontierOp
	GetAcceptedFrontierFailedOp
	AcceptedFrontierOp
	GetAcceptedOp
	GetAcceptedFailedOp
	AcceptedOp
	GetAncestorsOp
	GetAncestorsFailedOp
	AncestorsOp
	// Consensus:
	GetOp
	GetFailedOp
	PutOp
	PushQueryOp
	PullQueryOp
	QueryFailedOp
	ChitsOp
	// Application:
	AppRequestOp
	AppRequestFailedOp
	AppResponseOp
	AppGossipOp
	// Cross chain:
	CrossChainAppRequestOp
	CrossChainAppRequestFailedOp
	CrossChainAppResponseOp
	// Internal:
	ConnectedOp
	ConnectedSubnetOp
	DisconnectedOp
	NotifyOp
	GossipRequestOp
	TimeoutOp
)

var (
	HandshakeOps = []Op{
		PingOp,
		PongOp,
		VersionOp,
		PeerListOp,
		PeerListAckOp,
	}

	// List of all consensus request message types
	ConsensusRequestOps = []Op{
		GetStateSummaryFrontierOp,
		GetAcceptedStateSummaryOp,
		GetAcceptedFrontierOp,
		GetAcceptedOp,
		GetAncestorsOp,
		GetOp,
		PushQueryOp,
		PullQueryOp,
		AppRequestOp,
	}
	ConsensusResponseOps = []Op{
		StateSummaryFrontierOp,
		AcceptedStateSummaryOp,
		AcceptedFrontierOp,
		AcceptedOp,
		AncestorsOp,
		PutOp,
		ChitsOp,
		AppResponseOp,
	}
	// AppGossip is the only message that is sent unrequested without the
	// expectation of a response
	ConsensusExternalOps = append(
		ConsensusRequestOps,
		append(
			ConsensusResponseOps,
			AppGossipOp,
		)...,
	)
	ConsensusInternalOps = []Op{
		GetStateSummaryFrontierFailedOp,
		GetAcceptedStateSummaryFailedOp,
		GetAcceptedFrontierFailedOp,
		GetAcceptedFailedOp,
		GetAncestorsFailedOp,
		GetFailedOp,
		QueryFailedOp,
		AppRequestFailedOp,
		CrossChainAppRequestOp,
		CrossChainAppRequestFailedOp,
		CrossChainAppResponseOp,
		ConnectedOp,
		ConnectedSubnetOp,
		DisconnectedOp,
		NotifyOp,
		GossipRequestOp,
		TimeoutOp,
	}
	ConsensusOps = append(ConsensusExternalOps, ConsensusInternalOps...)

	ExternalOps = append(ConsensusExternalOps, HandshakeOps...)

	SynchronousOps = []Op{
		// State sync
		GetStateSummaryFrontierOp,
		GetStateSummaryFrontierFailedOp,
		StateSummaryFrontierOp,
		GetAcceptedStateSummaryOp,
		GetAcceptedStateSummaryFailedOp,
		AcceptedStateSummaryOp,
		// Bootstrapping
		GetAcceptedFrontierOp,
		GetAcceptedFrontierFailedOp,
		AcceptedFrontierOp,
		GetAcceptedOp,
		GetAcceptedFailedOp,
		AcceptedOp,
		GetAncestorsOp,
		GetAncestorsFailedOp,
		AncestorsOp,
		// Consensus
		GetOp,
		GetFailedOp,
		PutOp,
		PushQueryOp,
		PullQueryOp,
		QueryFailedOp,
		ChitsOp,
		// Internal
		ConnectedOp,
		ConnectedSubnetOp,
		DisconnectedOp,
	}

	AsynchronousOps = []Op{
		// Application
		AppRequestOp,
		AppRequestFailedOp,
		AppGossipOp,
		AppResponseOp,
		// Cross chain
		CrossChainAppRequestOp,
		CrossChainAppRequestFailedOp,
		CrossChainAppResponseOp,
	}

	FailedToResponseOps = map[Op]Op{
		GetStateSummaryFrontierFailedOp: StateSummaryFrontierOp,
		GetAcceptedStateSummaryFailedOp: AcceptedStateSummaryOp,
		GetAcceptedFrontierFailedOp:     AcceptedFrontierOp,
		GetAcceptedFailedOp:             AcceptedOp,
		GetAncestorsFailedOp:            AncestorsOp,
		GetFailedOp:                     PutOp,
		QueryFailedOp:                   ChitsOp,
		AppRequestFailedOp:              AppResponseOp,
		CrossChainAppRequestFailedOp:    CrossChainAppResponseOp,
	}
	UnrequestedOps = set.Set[Op]{
		GetAcceptedFrontierOp:     {},
		GetAcceptedOp:             {},
		GetAncestorsOp:            {},
		GetOp:                     {},
		PushQueryOp:               {},
		PullQueryOp:               {},
		AppRequestOp:              {},
		AppGossipOp:               {},
		CrossChainAppRequestOp:    {},
		GetStateSummaryFrontierOp: {},
		GetAcceptedStateSummaryOp: {},
	}

	errUnknownMessageType = errors.New("unknown message type")
)

func (op Op) String() string {
	switch op {
	// Handshake
	case PingOp:
		return "ping"
	case PongOp:
		return "pong"
	case VersionOp:
		return "version"
	case PeerListOp:
		return "peerlist"
	case PeerListAckOp:
		return "peerlist_ack"
	// State sync
	case GetStateSummaryFrontierOp:
		return "get_state_summary_frontier"
	case GetStateSummaryFrontierFailedOp:
		return "get_state_summary_frontier_failed"
	case StateSummaryFrontierOp:
		return "state_summary_frontier"
	case GetAcceptedStateSummaryOp:
		return "get_accepted_state_summary"
	case GetAcceptedStateSummaryFailedOp:
		return "get_accepted_state_summary_failed"
	case AcceptedStateSummaryOp:
		return "accepted_state_summary"
	// Bootstrapping
	case GetAcceptedFrontierOp:
		return "get_accepted_frontier"
	case GetAcceptedFrontierFailedOp:
		return "get_accepted_frontier_failed"
	case AcceptedFrontierOp:
		return "accepted_frontier"
	case GetAcceptedOp:
		return "get_accepted"
	case GetAcceptedFailedOp:
		return "get_accepted_failed"
	case AcceptedOp:
		return "accepted"
	case GetAncestorsOp:
		return "get_ancestors"
	case GetAncestorsFailedOp:
		return "get_ancestors_failed"
	case AncestorsOp:
		return "ancestors"
	// Consensus
	case GetOp:
		return "get"
	case GetFailedOp:
		return "get_failed"
	case PutOp:
		return "put"
	case PushQueryOp:
		return "push_query"
	case PullQueryOp:
		return "pull_query"
	case QueryFailedOp:
		return "query_failed"
	case ChitsOp:
		return "chits"
	// Application
	case AppRequestOp:
		return "app_request"
	case AppRequestFailedOp:
		return "app_request_failed"
	case AppResponseOp:
		return "app_response"
	case AppGossipOp:
		return "app_gossip"
	// Cross chain
	case CrossChainAppRequestOp:
		return "cross_chain_app_request"
	case CrossChainAppRequestFailedOp:
		return "cross_chain_app_request_failed"
	case CrossChainAppResponseOp:
		return "cross_chain_app_response"
		// Internal
	case ConnectedOp:
		return "connected"
	case ConnectedSubnetOp:
		return "connected_subnet"
	case DisconnectedOp:
		return "disconnected"
	case NotifyOp:
		return "notify"
	case GossipRequestOp:
		return "gossip_request"
	case TimeoutOp:
		return "timeout"
	default:
		return "unknown"
	}
}

func Unwrap(m *p2p.Message) (interface{}, error) {
	switch msg := m.GetMessage().(type) {
	// Handshake:
	case *p2p.Message_Ping:
		return msg.Ping, nil
	case *p2p.Message_Pong:
		return msg.Pong, nil
	case *p2p.Message_Version:
		return msg.Version, nil
	case *p2p.Message_PeerList:
		return msg.PeerList, nil
	case *p2p.Message_PeerListAck:
		return msg.PeerListAck, nil
	// State sync:
	case *p2p.Message_GetStateSummaryFrontier:
		return msg.GetStateSummaryFrontier, nil
	case *p2p.Message_StateSummaryFrontier_:
		return msg.StateSummaryFrontier_, nil
	case *p2p.Message_GetAcceptedStateSummary:
		return msg.GetAcceptedStateSummary, nil
	case *p2p.Message_AcceptedStateSummary_:
		return msg.AcceptedStateSummary_, nil
	// Bootstrapping:
	case *p2p.Message_GetAcceptedFrontier:
		return msg.GetAcceptedFrontier, nil
	case *p2p.Message_AcceptedFrontier_:
		return msg.AcceptedFrontier_, nil
	case *p2p.Message_GetAccepted:
		return msg.GetAccepted, nil
	case *p2p.Message_Accepted_:
		return msg.Accepted_, nil
	case *p2p.Message_GetAncestors:
		return msg.GetAncestors, nil
	case *p2p.Message_Ancestors_:
		return msg.Ancestors_, nil
	// Consensus:
	case *p2p.Message_Get:
		return msg.Get, nil
	case *p2p.Message_Put:
		return msg.Put, nil
	case *p2p.Message_PushQuery:
		return msg.PushQuery, nil
	case *p2p.Message_PullQuery:
		return msg.PullQuery, nil
	case *p2p.Message_Chits:
		return msg.Chits, nil
	// Application:
	case *p2p.Message_AppRequest:
		return msg.AppRequest, nil
	case *p2p.Message_AppResponse:
		return msg.AppResponse, nil
	case *p2p.Message_AppGossip:
		return msg.AppGossip, nil
	default:
		return nil, fmt.Errorf("%w: %T", errUnknownMessageType, msg)
	}
}

func ToOp(m *p2p.Message) (Op, error) {
	switch msg := m.GetMessage().(type) {
	case *p2p.Message_Ping:
		return PingOp, nil
	case *p2p.Message_Pong:
		return PongOp, nil
	case *p2p.Message_Version:
		return VersionOp, nil
	case *p2p.Message_PeerList:
		return PeerListOp, nil
	case *p2p.Message_PeerListAck:
		return PeerListAckOp, nil
	case *p2p.Message_GetStateSummaryFrontier:
		return GetStateSummaryFrontierOp, nil
	case *p2p.Message_StateSummaryFrontier_:
		return StateSummaryFrontierOp, nil
	case *p2p.Message_GetAcceptedStateSummary:
		return GetAcceptedStateSummaryOp, nil
	case *p2p.Message_AcceptedStateSummary_:
		return AcceptedStateSummaryOp, nil
	case *p2p.Message_GetAcceptedFrontier:
		return GetAcceptedFrontierOp, nil
	case *p2p.Message_AcceptedFrontier_:
		return AcceptedFrontierOp, nil
	case *p2p.Message_GetAccepted:
		return GetAcceptedOp, nil
	case *p2p.Message_Accepted_:
		return AcceptedOp, nil
	case *p2p.Message_GetAncestors:
		return GetAncestorsOp, nil
	case *p2p.Message_Ancestors_:
		return AncestorsOp, nil
	case *p2p.Message_Get:
		return GetOp, nil
	case *p2p.Message_Put:
		return PutOp, nil
	case *p2p.Message_PushQuery:
		return PushQueryOp, nil
	case *p2p.Message_PullQuery:
		return PullQueryOp, nil
	case *p2p.Message_Chits:
		return ChitsOp, nil
	case *p2p.Message_AppRequest:
		return AppRequestOp, nil
	case *p2p.Message_AppResponse:
		return AppResponseOp, nil
	case *p2p.Message_AppGossip:
		return AppGossipOp, nil
	default:
		return 0, fmt.Errorf("%w: %T", errUnknownMessageType, msg)
	}
}
