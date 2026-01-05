// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"net"
	"net/netip"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// Certificate length, signature length, IP, timestamp, tx ID
	baseIPCertDescLen = 2*wrappers.IntLen + net.IPv6len + wrappers.ShortLen + wrappers.LongLen + ids.IDLen
	preimageLen       = ids.IDLen + wrappers.LongLen
)

// A self contained proof that a peer is claiming ownership of an IPPort at a
// given time.
type ClaimedIPPort struct {
	// The peer's certificate.
	Cert *staking.Certificate
	// The peer's claimed IP and port.
	AddrPort netip.AddrPort
	// The time the peer claimed to own this IP and port.
	Timestamp uint64
	// [Cert]'s signature over the IPPort and timestamp.
	// This is used in the networking library to ensure that this IPPort was
	// actually claimed by the peer in question, and not by a malicious peer
	// trying to get us to dial bogus IPPorts.
	Signature []byte
	// NodeID derived from the peer certificate.
	NodeID ids.NodeID
	// GossipID derived from the nodeID and timestamp.
	GossipID ids.ID
}

func NewClaimedIPPort(
	cert *staking.Certificate,
	ipPort netip.AddrPort,
	timestamp uint64,
	signature []byte,
) *ClaimedIPPort {
	ip := &ClaimedIPPort{
		Cert:      cert,
		AddrPort:  ipPort,
		Timestamp: timestamp,
		Signature: signature,
		NodeID:    ids.NodeIDFromCert(cert),
	}

	packer := wrappers.Packer{
		Bytes: make([]byte, preimageLen),
	}
	packer.PackFixedBytes(ip.NodeID[:])
	packer.PackLong(timestamp)
	ip.GossipID = hashing.ComputeHash256Array(packer.Bytes)
	return ip
}

// Returns the approximate size of the binary representation of this ClaimedIPPort.
func (i *ClaimedIPPort) Size() int {
	return baseIPCertDescLen + len(i.Cert.Raw) + len(i.Signature)
}
