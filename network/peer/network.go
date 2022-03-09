// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils"
)

// Network defines the interface that is used by a peer to help establish a well
// connected p2p network.
type Network interface {
	// Connected is called by the peer once the handshake is finished.
	Connected(ids.ShortID)

	// AllowConnection enables the network is signal to the peer that its
	// connection is no longer desired and should be terminated.
	AllowConnection(ids.ShortID) bool

	// Track allows the peer to notify the network of a potential new peer to
	// connect to.
	Track(utils.IPCertDesc)

	// Disconnected is called when the peer finishes shutting down. It is not
	// guaranteed that [Connected] was called for the provided peer. However, it
	// is guaranteed that [Connected] will not be called after [Disconnected]
	// for a given [Peer] object.
	Disconnected(ids.ShortID)

	// Version provides the peer with the Version message to send to the peer
	// during the handshake.
	Version() (message.OutboundMessage, error)

	// Peers provides the peer with the PeerList message to send to the peer
	// during the handshake.
	Peers() (message.OutboundMessage, error)

	// Pong provides the peer with a Pong message to send to the peer in
	// response to a Ping message.
	Pong(ids.ShortID) (message.OutboundMessage, error)
}
