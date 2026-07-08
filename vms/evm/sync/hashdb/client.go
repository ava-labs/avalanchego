// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// Client sends state-trie leaf-range requests. Range-proof verification
// is the caller's responsibility.
type Client = network.Dispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// NewClient binds a [Client] at [p2p.EVMLeafRequestHandlerID] on n.
func NewClient(n *p2p.Network, peers *p2p.PeerTracker, id uint64) *Client {
	return network.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
		n,
		id,
		peers,
	)
}
