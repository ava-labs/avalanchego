// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// Client sends code-by-hash requests.
type Client = network.Dispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse]

// NewClient binds a [Client] at [p2p.EVMCodeRequestHandlerID] on p2pNet.
func NewClient(p2pNet *p2p.Network, peers *p2p.PeerTracker) *Client {
	return network.NewDispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse](
		network.NewClient(p2pNet, p2p.EVMCodeRequestHandlerID),
		peers,
	)
}
