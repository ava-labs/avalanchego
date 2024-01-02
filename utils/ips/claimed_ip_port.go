// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// Certificate length, signature length, IP, timestamp, tx ID
	baseIPCertDescLen = 2*wrappers.IntLen + IPPortLen + wrappers.LongLen + ids.IDLen
	preimageLen       = ids.IDLen + IPPortLen + wrappers.LongLen
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
	// The txID that added this peer into the validator set
	TxID ids.ID
}

func (i *ClaimedIPPort) NodeID() ids.NodeID {
	// TODO: Don't recalculate
	return ids.NodeIDFromCert(i.Cert)
}

func (i *ClaimedIPPort) GossipID() ids.ID {
	// TODO: Don't recalculate
	packer := wrappers.Packer{
		Bytes: make([]byte, preimageLen),
	}
	nodeID := i.NodeID()
	packer.PackFixedBytes(nodeID[:])
	PackIP(&packer, i.IPPort)
	packer.PackLong(i.Timestamp)
	return hashing.ComputeHash256Array(packer.Bytes)
}

// Returns the length of the byte representation of this ClaimedIPPort.
func (i *ClaimedIPPort) BytesLen() int {
	return baseIPCertDescLen + len(i.Cert.Raw) + len(i.Signature)
}
