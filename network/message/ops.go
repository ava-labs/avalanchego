// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

// Op is an opcode
type Op byte

// Types of messages that may be sent between nodes
// Note: If you add a new Op below, you must also add it to ops (declared below)
const (
	// Handshake:
	GetVersion Op = iota
	_
	GetPeerList
	_
	Ping
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
	Version
	PeerList
)

var (
	// List of all message types
	ops = []Op{
		GetVersion,
		GetPeerList,
		Ping,
		Pong,
		GetAcceptedFrontier,
		AcceptedFrontier,
		GetAccepted,
		Accepted,
		GetAncestors,
		MultiPut,
		Get,
		Put,
		PushQuery,
		PullQuery,
		Chits,
		Version,
		PeerList,
	}

	// Defines the messages that can be sent/received with this network
	messages = map[Op][]Field{
		// Handshake:
		GetVersion:  {},
		Version:     {NetworkID, NodeID, MyTime, IP, VersionStr, VersionTime, SigBytes},
		GetPeerList: {},
		PeerList:    {SignedPeers},
		Ping:        {},
		Pong:        {},
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
	}
)

func (op Op) Compressable() bool {
	switch op {
	case PeerList, Put, MultiPut, PushQuery:
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
	default:
		return "Unknown Op"
	}
}
