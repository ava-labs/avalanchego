// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

// Op is an opcode
type Op byte

// Types of messages that may be sent between nodes
// Note: If you add a new parseable Op below, you must also add it to ops
// (declared below)
const (
	// Handshake:
	GetVersion Op = iota
	_
	GetPeerList
	// TODO: NetworkUpgrade/Rename this to Pong
	UptimePong
	Ping
	// TODO: NetworkUpgrade/delete this in favor of UptimePong
	Pong
	// Bootstrapping:
	GetAcceptedFrontier
	AcceptedFrontier
	GetAccepted
	Accepted
	GetAncestors
	MultiPut
	// Consensus:
	Get
	Put
	PushQuery
	PullQuery
	Chits
	// Handshake / peer gossiping
	_
	PeerList
	Version
	// Application level:
	AppRequest
	AppResponse
	AppGossip

	// Internal messages (External messages should be added above these):
	GetAcceptedFrontierFailed
	GetAcceptedFailed
	GetFailed
	QueryFailed
	GetAncestorsFailed
	AppRequestFailed
	Timeout
	Connected
	Disconnected
	Notify
	GossipRequest
)

var (
	HandshakeOps = []Op{
		GetVersion,
		Version,
		GetPeerList,
		PeerList,
		Ping,
		Pong,
		UptimePong,
	}

	// List of all consensus request message types
	ConsensusRequestOps = []Op{
		GetAcceptedFrontier,
		GetAccepted,
		GetAncestors,
		Get,
		PushQuery,
		PullQuery,
		AppRequest,
	}
	ConsensusResponseOps = []Op{
		AcceptedFrontier,
		Accepted,
		MultiPut,
		Put,
		Chits,
		AppResponse,
	}
	// AppGossip is the only message that is sent unrequested without the
	// expectation of a response
	ConsensusExternalOps = append(
		ConsensusRequestOps,
		append(
			ConsensusResponseOps,
			AppGossip,
		)...,
	)
	ConsensusInternalOps = []Op{
		GetAcceptedFrontierFailed,
		GetAcceptedFailed,
		GetFailed,
		QueryFailed,
		GetAncestorsFailed,
		AppRequestFailed,
		Timeout,
		Connected,
		Disconnected,
		Notify,
		GossipRequest,
	}
	ConsensusOps = append(ConsensusExternalOps, ConsensusInternalOps...)

	ExternalOps = append(ConsensusExternalOps, HandshakeOps...)

	RequestToResponseOps = map[Op]Op{
		GetAcceptedFrontier: AcceptedFrontier,
		GetAccepted:         Accepted,
		GetAncestors:        MultiPut,
		Get:                 Put,
		PushQuery:           Chits,
		PullQuery:           Chits,
		AppRequest:          AppResponse,
	}
	ResponseToFailedOps = map[Op]Op{
		AcceptedFrontier: GetAcceptedFrontierFailed,
		Accepted:         GetAcceptedFailed,
		MultiPut:         GetAncestorsFailed,
		Put:              GetFailed,
		Chits:            QueryFailed,
		AppResponse:      AppRequestFailed,
	}
	FailedToResponseOps = map[Op]Op{
		GetAcceptedFrontierFailed: AcceptedFrontier,
		GetAcceptedFailed:         Accepted,
		GetAncestorsFailed:        MultiPut,
		GetFailed:                 Put,
		QueryFailed:               Chits,
		AppRequestFailed:          AppResponse,
	}
	UnrequestedOps = map[Op]struct{}{
		GetAcceptedFrontier: {},
		GetAccepted:         {},
		GetAncestors:        {},
		Get:                 {},
		PushQuery:           {},
		PullQuery:           {},
		AppRequest:          {},
		AppGossip:           {},
	}

	// Defines the messages that can be sent/received with this network
	messages = map[Op][]Field{
		// Handshake:
		GetVersion:  {},
		Version:     {NetworkID, NodeID, MyTime, IP, VersionStr, VersionTime, SigBytes, TrackedSubnets},
		GetPeerList: {},
		PeerList:    {SignedPeers},
		Ping:        {},
		Pong:        {},
		UptimePong:  {Uptime},
		// Bootstrapping:
		GetAcceptedFrontier: {ChainID, RequestID, Deadline},
		AcceptedFrontier:    {ChainID, RequestID, ContainerIDs},
		GetAccepted:         {ChainID, RequestID, Deadline, ContainerIDs},
		Accepted:            {ChainID, RequestID, ContainerIDs},
		GetAncestors:        {ChainID, RequestID, Deadline, ContainerID},
		MultiPut:            {ChainID, RequestID, MultiContainerBytes},
		// Consensus:
		Get:       {ChainID, RequestID, Deadline, ContainerID},
		Put:       {ChainID, RequestID, ContainerID, ContainerBytes},
		PushQuery: {ChainID, RequestID, Deadline, ContainerID, ContainerBytes},
		PullQuery: {ChainID, RequestID, Deadline, ContainerID},
		Chits:     {ChainID, RequestID, ContainerIDs},
		// Application level:
		AppRequest:  {ChainID, RequestID, Deadline, AppBytes},
		AppResponse: {ChainID, RequestID, AppBytes},
		AppGossip:   {ChainID, AppBytes},
	}
)

func (op Op) Compressable() bool {
	switch op {
	case PeerList, Put, MultiPut, PushQuery, AppRequest, AppResponse, AppGossip:
		return true
	default:
		return false
	}
}

func (op Op) String() string {
	switch op {
	case GetVersion:
		return "get_version"
	case Version:
		return "version"
	case GetPeerList:
		return "get_peerlist"
	case PeerList:
		return "peerlist"
	case Ping:
		return "ping"
	case Pong:
		return "pong"
	case UptimePong:
		return "uptime_pong"
	case GetAcceptedFrontier:
		return "get_accepted_frontier"
	case AcceptedFrontier:
		return "accepted_frontier"
	case GetAccepted:
		return "get_accepted"
	case Accepted:
		return "accepted"
	case Get:
		return "get"
	case GetAncestors:
		return "get_ancestors"
	case Put:
		return "put"
	case MultiPut:
		return "multi_put"
	case PushQuery:
		return "push_query"
	case PullQuery:
		return "pull_query"
	case Chits:
		return "chits"
	case AppRequest:
		return "app_request"
	case AppResponse:
		return "app_response"
	case AppGossip:
		return "app_gossip"

	case GetAcceptedFrontierFailed:
		return "get_accepted_frontier_failed"
	case GetAcceptedFailed:
		return "get_accepted_failed"
	case GetFailed:
		return "get_failed"
	case QueryFailed:
		return "query_failed"
	case GetAncestorsFailed:
		return "get_ancestors_failed"
	case AppRequestFailed:
		return "app_request_failed"
	case Timeout:
		return "timeout"
	case Connected:
		return "connected"
	case Disconnected:
		return "disconnected"
	case Notify:
		return "notify"
	case GossipRequest:
		return "gossip_request"
	default:
		return "Unknown Op"
	}
}
