// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	HandshakeOp
	GetPeerListOp
	PeerListOp
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
	AppErrorOp
	AppResponseOp
	AppGossipOp
	// Internal:
	ConnectedOp
	DisconnectedOp
	NotifyOp
	GossipRequestOp
	TimeoutOp
)

var (
	HandshakeOps = []Op{
		PingOp,
		PongOp,
		HandshakeOp,
		GetPeerListOp,
		PeerListOp,
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
		AppErrorOp,
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
		ConnectedOp,
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
		DisconnectedOp,
	}

	AsynchronousOps = []Op{
		// Application
		AppRequestOp,
		AppErrorOp,
		AppGossipOp,
		AppResponseOp,
	}

	FailedToResponseOps = map[Op]Op{
		GetStateSummaryFrontierFailedOp: StateSummaryFrontierOp,
		GetAcceptedStateSummaryFailedOp: AcceptedStateSummaryOp,
		GetAcceptedFrontierFailedOp:     AcceptedFrontierOp,
		GetAcceptedFailedOp:             AcceptedOp,
		GetAncestorsFailedOp:            AncestorsOp,
		GetFailedOp:                     PutOp,
		QueryFailedOp:                   ChitsOp,
		AppErrorOp:                      AppResponseOp,
	}
	UnrequestedOps = set.Of(
		GetAcceptedFrontierOp,
		GetAcceptedOp,
		GetAncestorsOp,
		GetOp,
		PushQueryOp,
		PullQueryOp,
		AppRequestOp,
		AppGossipOp,
		GetStateSummaryFrontierOp,
		GetAcceptedStateSummaryOp,
	)

	errUnknownMessageType = errors.New("unknown message type")
)

func (op Op) String() string {
	switch op {
	// Handshake
	case PingOp:
		return "ping"
	case PongOp:
		return "pong"
	case HandshakeOp:
		return "handshake"
	case GetPeerListOp:
		return "get_peerlist"
	case PeerListOp:
		return "peerlist"
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
	case AppErrorOp:
		return "app_error"
	case AppResponseOp:
		return "app_response"
	case AppGossipOp:
		return "app_gossip"
	// Internal
	case ConnectedOp:
		return "connected"
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

func Unwrap(m *p2p.Message) (fmt.Stringer, error) {
	switch msg := m.GetMessage().(type) {
	// Handshake:
	case *p2p.Message_Ping:
		return msg.Ping, nil
	case *p2p.Message_Pong:
		return msg.Pong, nil
	case *p2p.Message_Handshake:
		return msg.Handshake, nil
	case *p2p.Message_GetPeerList:
		return msg.GetPeerList, nil
	case *p2p.Message_PeerList_:
		return msg.PeerList_, nil
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
	case *p2p.Message_AppError:
		return msg.AppError, nil
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
	case *p2p.Message_Handshake:
		return HandshakeOp, nil
	case *p2p.Message_GetPeerList:
		return GetPeerListOp, nil
	case *p2p.Message_PeerList_:
		return PeerListOp, nil
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
	case *p2p.Message_AppError:
		return AppErrorOp, nil
	case *p2p.Message_AppGossip:
		return AppGossipOp, nil
	default:
		return 0, fmt.Errorf("%w: %T", errUnknownMessageType, msg)
	}
}
