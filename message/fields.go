// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	case NetworkID:
		return wrappers.TryPackInt
	case NodeID:
		return wrappers.TryPackInt
	case MyTime:
		return wrappers.TryPackLong
	case IP:
		return wrappers.TryPackIP
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
	case AppBytes:
		return wrappers.TryPackBytes
	case SigBytes:
		return wrappers.TryPackBytes
	case VersionTime:
		return wrappers.TryPackLong
	case Peers:
		return wrappers.TryPackClaimedIPPortList
	case TrackedSubnets:
		return wrappers.TryPackHashes
	case Uptime:
		return wrappers.TryPackByte
	case SummaryBytes:
		return wrappers.TryPackBytes
	case SummaryHeights:
		return wrappers.TryPackUint64Slice
	case SummaryIDs:
		return wrappers.TryPackHashes
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
	case AppBytes:
		return wrappers.TryUnpackBytes
	case SigBytes:
		return wrappers.TryUnpackBytes
	case VersionTime:
		return wrappers.TryUnpackLong
	case Peers:
		return wrappers.TryUnpackClaimedIPPortList
	case TrackedSubnets:
		return wrappers.TryUnpackHashes
	case Uptime:
		return wrappers.TryUnpackByte
	case SummaryBytes:
		return wrappers.TryUnpackBytes
	case SummaryHeights:
		return wrappers.TryUnpackUint64Slice
	case SummaryIDs:
		return wrappers.TryUnpackHashes
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
