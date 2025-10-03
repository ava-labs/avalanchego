// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Network defines the interface that is used by a peer to help establish a well
// connected p2p network.
type Network interface {
	// Connected is called by the peer once the handshake is finished.
	Connected(peerID ids.NodeID)

	// AllowConnection enables the network is signal to the peer that its
	// connection is no longer desired and should be terminated.
	AllowConnection(peerID ids.NodeID) bool

	// Track allows the peer to notify the network of potential new peers to
	// connect to.
	Track(ctx context.Context, ips []*ips.ClaimedIPPort) error

	// Disconnected is called when the peer finishes shutting down. It is not
	// guaranteed that [Connected] was called for the provided peer. However, it
	// is guaranteed that [Connected] will not be called after [Disconnected]
	// for a given [Peer] object.
	Disconnected(ctx context.Context, peerID ids.NodeID)

	// KnownPeers returns the bloom filter of the known peers.
	KnownPeers() (bloomFilter []byte, salt []byte)

	// Peers returns peers that are not known.
	Peers(
		peerID ids.NodeID,
		trackedSubnets set.Set[ids.ID],
		requestAllPeers bool,
		knownPeers *bloom.ReadFilter,
		peerSalt []byte,
	) []*ips.ClaimedIPPort
}
