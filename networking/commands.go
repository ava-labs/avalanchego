// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/utils/wrappers"
)

// Field that may be packed into a message
type Field uint32

// Fields that may be packed. These values are not sent over the wire.
const (
	VersionStr     Field = iota // Used in handshake
	NetworkID                   // Used in handshake
	MyTime                      // Used in handshake
	IP                          // Used in handshake
	Peers                       // Used in handshake
	ChainID                     // Used for dispatching
	RequestID                   // Used for all messages
	ContainerID                 // Used for querying
	ContainerBytes              // Used for gossiping
	ContainerIDs                // Used for querying
	Bytes                       // Used as arbitrary data
	TxID                        // Used for throughput tests
	Tx                          // Used for throughput tests
	Status                      // Used for throughput tests
)

// Packer returns the packer function that can be used to pack this field.
func (f Field) Packer() func(*wrappers.Packer, interface{}) {
	switch f {
	case VersionStr:
		return wrappers.TryPackStr
	case NetworkID:
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
	case ContainerID:
		return wrappers.TryPackHash
	case ContainerBytes:
		return wrappers.TryPackBytes
	case ContainerIDs:
		return wrappers.TryPackHashes
	case Bytes:
		return wrappers.TryPackBytes
	case TxID:
		return wrappers.TryPackHash
	case Tx:
		return wrappers.TryPackBytes
	case Status:
		return wrappers.TryPackInt
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
	case ContainerID:
		return wrappers.TryUnpackHash
	case ContainerBytes:
		return wrappers.TryUnpackBytes
	case ContainerIDs:
		return wrappers.TryUnpackHashes
	case Bytes:
		return wrappers.TryUnpackBytes
	case TxID:
		return wrappers.TryUnpackHash
	case Tx:
		return wrappers.TryUnpackBytes
	case Status:
		return wrappers.TryUnpackInt
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
	case MyTime:
		return "MyTime"
	case IP:
		return "IP"
	case Peers:
		return "Peers"
	case ChainID:
		return "ChainID"
	case ContainerID:
		return "ContainerID"
	case ContainerBytes:
		return "Container Bytes"
	case ContainerIDs:
		return "Container IDs"
	case Bytes:
		return "Bytes"
	case TxID:
		return "TxID"
	case Tx:
		return "Tx"
	case Status:
		return "Status"
	default:
		return "Unknown Field"
	}
}

// Public commands that may be sent between stakers
const (
	// Handshake:
	GetVersion salticidae.Opcode = iota
	Version
	GetPeerList
	PeerList
	// Bootstrapping:
	GetAcceptedFrontier
	AcceptedFrontier
	GetAccepted
	Accepted
	// Consensus:
	Get
	Put
	PushQuery
	PullQuery
	Chits
	// Pinging:
	Ping
	Pong
	// Arbitrary data message:
	Data
	// Throughput test:
	IssueTx
	DecidedTx
)

// Defines the messages that can be sent/received with this network
var (
	Messages = map[salticidae.Opcode][]Field{
		// Handshake:
		GetVersion:  []Field{},
		Version:     []Field{NetworkID, MyTime, IP, VersionStr},
		GetPeerList: []Field{},
		PeerList:    []Field{Peers},
		// Bootstrapping:
		GetAcceptedFrontier: []Field{ChainID, RequestID},
		AcceptedFrontier:    []Field{ChainID, RequestID, ContainerIDs},
		GetAccepted:         []Field{ChainID, RequestID, ContainerIDs},
		Accepted:            []Field{ChainID, RequestID, ContainerIDs},
		// Consensus:
		Get:       []Field{ChainID, RequestID, ContainerID},
		Put:       []Field{ChainID, RequestID, ContainerID, ContainerBytes},
		PushQuery: []Field{ChainID, RequestID, ContainerID, ContainerBytes},
		PullQuery: []Field{ChainID, RequestID, ContainerID},
		Chits:     []Field{ChainID, RequestID, ContainerIDs},
		// Pinging:
		Ping: []Field{},
		Pong: []Field{},
		// Arbitrary data message:
		Data: []Field{Bytes},
		// Throughput test:
		IssueTx:   []Field{ChainID, Tx},
		DecidedTx: []Field{TxID, Status},
	}
)
