// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// Certificate length, signature length, IP, timestamp, tx ID
	baseIPCertDescLen = 2*wrappers.IntLen + IPPortLen + wrappers.LongLen + ids.IDLen
	preimageLen       = ids.IDLen + wrappers.LongLen
)

// A self contained proof that a peer is claiming ownership of an IPPort at a
// given time.
type ClaimedIPPort struct {
	// The peer's certificate.
	Cert *staking.Certificate
	// The peer's claimed IP and port.
	IPPort IPPort
	// The time the peer claimed to own this IP and port.
	Timestamp uint64
	// [Cert]'s signature over the IPPort and timestamp.
	// This is used in the networking library to ensure that this IPPort was
	// actually claimed by the peer in question, and not by a malicious peer
	// trying to get us to dial bogus IPPorts.
	Signature []byte

	initOnce sync.Once
	nodeID   ids.NodeID
	gossipID ids.ID
}

func (i *ClaimedIPPort) NodeID() ids.NodeID {
	i.initOnce.Do(i.init)
	return i.nodeID
}

func (i *ClaimedIPPort) GossipID() ids.ID {
	i.initOnce.Do(i.init)
	return i.gossipID
}

// Returns the length of the byte representation of this ClaimedIPPort.
func (i *ClaimedIPPort) BytesLen() int {
	return baseIPCertDescLen + len(i.Cert.Raw) + len(i.Signature)
}

func (i *ClaimedIPPort) init() {
	i.nodeID = ids.NodeIDFromCert(i.Cert)

	packer := wrappers.Packer{
		Bytes: make([]byte, preimageLen),
	}
	packer.PackFixedBytes(i.nodeID[:])
	packer.PackLong(i.Timestamp)
	i.gossipID = hashing.ComputeHash256Array(packer.Bytes)
}
