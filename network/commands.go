// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Field that may be packed into a message
type Field uint32

// Fields that may be packed. These values are not sent over the wire.
const (
	VersionStr          Field = iota // Used in handshake
	NetworkID                        // Used in handshake
	NodeID                           // Used in handshake
	MyTime                           // Used in handshake
	IP                               // Used in handshake
	Peers                            // Used in handshake
	ChainID                          // Used for dispatching
	RequestID                        // Used for all messages
	Deadline                         // Used for request messages
	ContainerID                      // Used for querying
	ContainerBytes                   // Used for gossiping
	ContainerIDs                     // Used for querying
	MultiContainerBytes              // Used in MultiPut
	SigBytes                         // Used in handshake / peer gossiping
	VersionTime                      // Used in handshake / peer gossiping
	SignedPeers                      // Used in peer gossiping
)

// Packer returns the packer function that can be used to pack this field.
func (f Field) Packer() func(*wrappers.Packer, interface{}) {
	switch f {
	case VersionStr:
		return wrappers.TryPackStr
	case NetworkID:
		return wrappers.TryPackInt
	case NodeID:
		return wrappers.TryPackInt
	case MyTime:
		return wrappers.TryPackLong
	case IP:
		return wrappers.TryPackIP
	case Peers:
		return wrappers.TryPackIPList
	case ChainID: // TODO: This will be shortened to use a modified varint spec
		return wrappers.TryPackHash
	case RequestID:
		return wrappers.TryPackInt
	case Deadline:
		return wrappers.TryPackLong
	case ContainerID:
		return wrappers.TryPackHash
	case ContainerBytes:
		return wrappers.TryPackBytes
	case ContainerIDs:
		return wrappers.TryPackHashes
	case MultiContainerBytes:
		return wrappers.TryPack2DBytes
	case SigBytes:
		return wrappers.TryPackBytes
	case VersionTime:
		return wrappers.TryPackLong
	case SignedPeers:
		return wrappers.TryPackIPCertList
	default:
		return nil
	}
}

// Unpacker returns the unpacker function that can be used to unpack this field.
func (f Field) Unpacker() func(*wrappers.Packer) interface{} {
	switch f {
	case VersionStr:
		return wrappers.TryUnpackStr
	case NetworkID:
		return wrappers.TryUnpackInt
	case NodeID:
		return wrappers.TryUnpackInt
	case MyTime:
		return wrappers.TryUnpackLong
	case IP:
		return wrappers.TryUnpackIP
	case Peers:
		return wrappers.TryUnpackIPList
	case ChainID: // TODO: This will be shortened to use a modified varint spec
		return wrappers.TryUnpackHash
	case RequestID:
		return wrappers.TryUnpackInt
	case Deadline:
		return wrappers.TryUnpackLong
	case ContainerID:
		return wrappers.TryUnpackHash
	case ContainerBytes:
		return wrappers.TryUnpackBytes
	case ContainerIDs:
		return wrappers.TryUnpackHashes
	case MultiContainerBytes:
		return wrappers.TryUnpack2DBytes
	case SigBytes:
		return wrappers.TryUnpackBytes
	case VersionTime:
		return wrappers.TryUnpackLong
	case SignedPeers:
		return wrappers.TryUnpackIPCertList
	default:
		return nil
	}
}

func (f Field) String() string {
	switch f {
	case VersionStr:
		return "VersionStr"
	case NetworkID:
		return "NetworkID"
	case NodeID:
		return "NodeID"
	case MyTime:
		return "MyTime"
	case IP:
		return "IP"
	case Peers:
		return "Peers"
	case ChainID:
		return "ChainID"
	case RequestID:
		return "RequestID"
	case Deadline:
		return "Deadline"
	case ContainerID:
		return "ContainerID"
	case ContainerBytes:
		return "Container Bytes"
	case ContainerIDs:
		return "Container IDs"
	case MultiContainerBytes:
		return "MultiContainerBytes"
	case SigBytes:
		return "SigBytes"
	case VersionTime:
		return "VersionTime"
	case SignedPeers:
		return "SignedPeers"
	default:
		return "Unknown Field"
	}
}

// Op is an opcode
type Op byte

func (op Op) Compressable() bool {
	switch op {
	case GetVersion:
		return false
	case Version:
		return false
	case GetPeerList:
		return false
	case PeerList:
		return true
	case Ping:
		return false
	case Pong:
		return false
	case GetAcceptedFrontier:
		return false
	case AcceptedFrontier:
		return false
	case GetAccepted:
		return false
	case Accepted:
		return false
	case Get:
		return false
	case GetAncestors:
		return false
	case Put:
		return true
	case MultiPut:
		return true
	case PushQuery:
		return true
	case PullQuery:
		return false
	case Chits:
		return false
	default:
		// we don't recognise the message
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

// List of all message types
var ops = []Op{
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
var (
	Messages = map[Op][]Field{
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
