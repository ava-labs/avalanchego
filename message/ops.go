// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

// Op is an opcode
type Op byte

// Types of messages that may be sent between nodes
// Note: If you add a new parseable Op below, you must also add it to ops
// (declared below)
//
// "_" are used in places where old message types were defined that are no
// longer supported. When new messages are introduced these values are typically
// safe to reuse.
const (
	// Handshake:
	_ Op = iota // Used to be a GetVersion message
	_           // Used to be a Version message
	_           // Used to be a GetPeerList message
	Pong
	Ping
	_ // Used to be a Pong message
	// Bootstrapping:
	GetAcceptedFrontier
	AcceptedFrontier
	GetAccepted
	Accepted
	GetAncestors
	Ancestors
	// Consensus:
	Get
	Put
	PushQuery
	PullQuery
	Chits
	// Handshake / peer gossiping
	_ // Used to be a Version message
	PeerList
	Version
	// Application level:
	AppRequest
	AppResponse
	AppGossip
	// State sync
	GetStateSummaryFrontier
	StateSummaryFrontier
	GetAcceptedStateSummary
	AcceptedStateSummary
	// linearize dag
	ChitsV2

	// Internal messages (External messages should be added above these):
	GetAcceptedFrontierFailed
	GetAcceptedFailed
	GetAncestorsFailed
	GetFailed
	QueryFailed
	AppRequestFailed
	Timeout
	Connected
	Disconnected
	Notify
	GossipRequest
	GetStateSummaryFrontierFailed
	GetAcceptedStateSummaryFailed
)

var (
	HandshakeOps = []Op{
		Version,
		PeerList,
		Ping,
		Pong,
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
		GetStateSummaryFrontier,
		GetAcceptedStateSummary,
	}
	ConsensusResponseOps = []Op{
		AcceptedFrontier,
		Accepted,
		Ancestors,
		Put,
		Chits,
		ChitsV2,
		AppResponse,
		StateSummaryFrontier,
		AcceptedStateSummary,
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
		GetAncestorsFailed,
		GetFailed,
		QueryFailed,
		AppRequestFailed,
		Timeout,
		Connected,
		Disconnected,
		Notify,
		GossipRequest,
		GetStateSummaryFrontierFailed,
		GetAcceptedStateSummaryFailed,
	}
	ConsensusOps = append(ConsensusExternalOps, ConsensusInternalOps...)

	ExternalOps = append(ConsensusExternalOps, HandshakeOps...)

	SynchronousOps = []Op{
		GetAcceptedFrontier,
		AcceptedFrontier,
		GetAccepted,
		Accepted,
		GetAncestors,
		Ancestors,
		Get,
		Put,
		PushQuery,
		PullQuery,
		Chits,
		ChitsV2,
		GetAcceptedFrontierFailed,
		GetAcceptedFailed,
		GetAncestorsFailed,
		GetFailed,
		QueryFailed,
		Connected,
		Disconnected,

		// State sync
		GetStateSummaryFrontier,
		StateSummaryFrontier,
		GetAcceptedStateSummary,
		AcceptedStateSummary,
		GetStateSummaryFrontierFailed,
		GetAcceptedStateSummaryFailed,
	}

	AsynchronousOps = []Op{
		AppRequest,
		AppGossip,
		AppRequestFailed,
		AppResponse,
	}

	RequestToResponseOps = map[Op]Op{
		GetAcceptedFrontier:     AcceptedFrontier,
		GetAccepted:             Accepted,
		GetAncestors:            Ancestors,
		Get:                     Put,
		PushQuery:               Chits,
		PullQuery:               Chits,
		AppRequest:              AppResponse,
		GetStateSummaryFrontier: StateSummaryFrontier,
		GetAcceptedStateSummary: AcceptedStateSummary,
	}
	ResponseToFailedOps = map[Op]Op{
		AcceptedFrontier:     GetAcceptedFrontierFailed,
		Accepted:             GetAcceptedFailed,
		Ancestors:            GetAncestorsFailed,
		Put:                  GetFailed,
		Chits:                QueryFailed,
		ChitsV2:              QueryFailed,
		AppResponse:          AppRequestFailed,
		StateSummaryFrontier: GetStateSummaryFrontierFailed,
		AcceptedStateSummary: GetAcceptedStateSummaryFailed,
	}
	FailedToResponseOps = map[Op]Op{
		GetAcceptedFrontierFailed: AcceptedFrontier,
		GetAcceptedFailed:         Accepted,
		GetAncestorsFailed:        Ancestors,
		GetFailed:                 Put,

		// "ChitV2" response failure also uses "Chits"
		QueryFailed: Chits,

		AppRequestFailed:              AppResponse,
		GetStateSummaryFrontierFailed: StateSummaryFrontier,
		GetAcceptedStateSummaryFailed: AcceptedStateSummary,
	}
	UnrequestedOps = map[Op]struct{}{
		GetAcceptedFrontier:     {},
		GetAccepted:             {},
		GetAncestors:            {},
		Get:                     {},
		PushQuery:               {},
		PullQuery:               {},
		AppRequest:              {},
		AppGossip:               {},
		GetStateSummaryFrontier: {},
		GetAcceptedStateSummary: {},
	}

	// Defines the messages that can be sent/received with this network
	messages = map[Op][]Field{
		// Handshake:
		// TODO: remove NodeID from the Version message
		Version:  {NetworkID, NodeID, MyTime, IP, VersionStr, VersionTime, SigBytes, TrackedSubnets},
		PeerList: {Peers},
		Ping:     {},
		Pong:     {Uptime},
		// Bootstrapping:
		GetAcceptedFrontier: {ChainID, RequestID, Deadline},
		AcceptedFrontier:    {ChainID, RequestID, ContainerIDs},
		GetAccepted:         {ChainID, RequestID, Deadline, ContainerIDs},
		Accepted:            {ChainID, RequestID, ContainerIDs},
		GetAncestors:        {ChainID, RequestID, Deadline, ContainerID},
		Ancestors:           {ChainID, RequestID, MultiContainerBytes},
		// Consensus:
		Get:       {ChainID, RequestID, Deadline, ContainerID},
		Put:       {ChainID, RequestID, ContainerID, ContainerBytes},
		PushQuery: {ChainID, RequestID, Deadline, ContainerID, ContainerBytes},
		PullQuery: {ChainID, RequestID, Deadline, ContainerID},
		Chits:     {ChainID, RequestID, ContainerIDs},

		// ChitV2 is used for transition from DAG to linear chain
		// First "ContainerIDs" field represents the votes from the existing DAG
		// Second "ContainerID" field represents the vote from newly instantiated snowman chain
		// The message sender should populate both, and the message handler should choose "one"
		// depending on the consensus engine it's running at the time.
		//
		// TODO: define a new chit message v3 with container ID as a single value
		//       once DAG is linearized
		ChitsV2: {ChainID, RequestID, ContainerIDs, ContainerID},

		// Application level:
		AppRequest:  {ChainID, RequestID, Deadline, AppBytes},
		AppResponse: {ChainID, RequestID, AppBytes},
		AppGossip:   {ChainID, AppBytes},
		// State Sync
		GetStateSummaryFrontier: {ChainID, RequestID, Deadline},
		StateSummaryFrontier:    {ChainID, RequestID, SummaryBytes},
		GetAcceptedStateSummary: {ChainID, RequestID, Deadline, SummaryHeights},
		AcceptedStateSummary:    {ChainID, RequestID, SummaryIDs},
	}
)

func (op Op) Compressible() bool {
	switch op {
	case PeerList, Put, Ancestors, PushQuery,
		AppRequest, AppResponse, AppGossip,
		StateSummaryFrontier, GetAcceptedStateSummary, AcceptedStateSummary:
		return true
	default:
		return false
	}
}

func (op Op) String() string {
	switch op {
	case Version:
		return "version"
	case PeerList:
		return "peerlist"
	case Ping:
		return "ping"
	case Pong:
		return "pong"
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
	case Ancestors:
		return "ancestors"
	case PushQuery:
		return "push_query"
	case PullQuery:
		return "pull_query"
	case Chits:
		return "chits"
	case ChitsV2:
		return "chits_v2"
	case AppRequest:
		return "app_request"
	case AppResponse:
		return "app_response"
	case AppGossip:
		return "app_gossip"
	case GetStateSummaryFrontier:
		return "get_state_summary_frontier"
	case StateSummaryFrontier:
		return "state_summary_frontier"
	case GetAcceptedStateSummary:
		return "get_accepted_state_summary"
	case AcceptedStateSummary:
		return "accepted_state_summary"

	case GetAcceptedFrontierFailed:
		return "get_accepted_frontier_failed"
	case GetAcceptedFailed:
		return "get_accepted_failed"
	case GetAncestorsFailed:
		return "get_ancestors_failed"
	case GetFailed:
		return "get_failed"
	case QueryFailed:
		return "query_failed"
	case AppRequestFailed:
		return "app_request_failed"
	case GetStateSummaryFrontierFailed:
		return "get_state_summary_frontier_failed"
	case GetAcceptedStateSummaryFailed:
		return "get_accepted_state_summary_failed"
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
