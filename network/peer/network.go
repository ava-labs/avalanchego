// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
)

// Network defines the interface that is used by a peer to help establish a well
// connected p2p network.
type Network interface {
	// Connected is called by the peer once the handshake is finished.
	Connected(ids.NodeID)

	// AllowConnection enables the network is signal to the peer that its
	// connection is no longer desired and should be terminated.
	AllowConnection(ids.NodeID) bool

	// Track allows the peer to notify the network of a potential new peer to
	// connect to.
	//
	// Returns false if this call was not "useful". That is, we were already
	// connected to this node, we already had this tracking information, the
	// signature is invalid or we don't want to connect.
	Track(ips.ClaimedIPPort) bool

	// Disconnected is called when the peer finishes shutting down. It is not
	// guaranteed that [Connected] was called for the provided peer. However, it
	// is guaranteed that [Connected] will not be called after [Disconnected]
	// for a given [Peer] object.
	Disconnected(ids.NodeID)

	// Peers returns peers that [peerID] might not know about.
	Peers(peerID ids.NodeID) ([]ips.ClaimedIPPort, error)
}
