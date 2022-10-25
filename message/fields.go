// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

// Field that may be packed into a message
type Field uint32

// Fields that may be packed. These values are not sent over the wire.
const (
	VersionStr          Field = iota // Used in handshake
	NetworkID                        // Used in handshake
	NodeID                           // TODO: remove NodeID. Used in handshake
	MyTime                           // Used in handshake
	IP                               // Used in handshake
	ChainID                          // Used for dispatching
	RequestID                        // Used for all messages
	Deadline                         // Used for request messages
	ContainerID                      // Used for querying
	ContainerBytes                   // Used for gossiping
	ContainerIDs                     // Used for querying
	MultiContainerBytes              // Used in Ancestors
	SigBytes                         // Used in handshake / peer gossiping
	VersionTime                      // Used in handshake / peer gossiping
	Peers                            // Used in peer gossiping
	TrackedSubnets                   // Used in handshake / peer gossiping
	AppBytes                         // Used at application level
	VMMessage                        // Used internally
	Uptime                           // Used for Pong
	SummaryBytes                     // Used for state sync
	SummaryHeights                   // Used for state sync
	SummaryIDs                       // Used for state sync
	VersionStruct                    // Used internally
	SourceChainID                    // Used for cross-chain messaging
)

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
	case ChainID:
		return "ChainID"
	case SourceChainID:
		return "SourceChainID"
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
	case AppBytes:
		return "AppBytes"
	case SigBytes:
		return "SigBytes"
	case VersionTime:
		return "VersionTime"
	case Peers:
		return "Peers"
	case TrackedSubnets:
		return "TrackedSubnets"
	case VMMessage:
		return "VMMessage"
	case Uptime:
		return "Uptime"
	case SummaryBytes:
		return "Summary"
	case SummaryHeights:
		return "SummaryHeights"
	case SummaryIDs:
		return "SummaryIDs"
	case VersionStruct:
		return "VersionStruct"
	default:
		return "Unknown Field"
	}
}
