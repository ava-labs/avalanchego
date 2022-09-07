// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

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
)

// Packer returns the packer function that can be used to pack this field.
func (f Field) Packer() func(*wrappers.Packer, interface{}) {
	switch f {
	case VersionStr:
		return wrappers.TryPackStr
	case NetworkID, NodeID, RequestID:
		return wrappers.TryPackInt
	case MyTime, Deadline, VersionTime:
		return wrappers.TryPackLong
	case IP:
		return wrappers.TryPackIP
	case ChainID, ContainerID: // TODO: This will be shortened to use a modified varint spec
		return wrappers.TryPackHash
	case ContainerBytes, AppBytes, SigBytes, SummaryBytes:
		return wrappers.TryPackBytes
	case ContainerIDs, TrackedSubnets, SummaryIDs:
		return wrappers.TryPackHashes
	case MultiContainerBytes:
		return wrappers.TryPack2DBytes
	case Peers:
		return wrappers.TryPackClaimedIPPortList
	case Uptime:
		return wrappers.TryPackByte
	case SummaryHeights:
		return wrappers.TryPackUint64Slice
	default:
		return nil
	}
}

// Unpacker returns the unpacker function that can be used to unpack this field.
func (f Field) Unpacker() func(*wrappers.Packer) interface{} {
	switch f {
	case VersionStr:
		return wrappers.TryUnpackStr
	case NetworkID, NodeID, RequestID:
		return wrappers.TryUnpackInt
	case MyTime, Deadline, VersionTime:
		return wrappers.TryUnpackLong
	case IP:
		return wrappers.TryUnpackIP
	case ChainID, ContainerID: // TODO: This will be shortened to use a modified varint spec
		return wrappers.TryUnpackHash
	case ContainerBytes, AppBytes, SigBytes, SummaryBytes:
		return wrappers.TryUnpackBytes
	case ContainerIDs, TrackedSubnets, SummaryIDs:
		return wrappers.TryUnpackHashes
	case MultiContainerBytes:
		return wrappers.TryUnpack2DBytes
	case Peers:
		return wrappers.TryUnpackClaimedIPPortList
	case Uptime:
		return wrappers.TryUnpackByte
	case SummaryHeights:
		return wrappers.TryUnpackUint64Slice
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
