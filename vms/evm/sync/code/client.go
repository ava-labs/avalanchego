// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// Client sends code-by-hash requests.
type Client = network.Dispatcher[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// NewClient binds a [Client] at [p2p.EVMCodeRequestHandlerID] on n.
func NewClient(n *p2p.Network, peers *p2p.PeerTracker) *Client {
	return network.NewDispatcher[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse](
		n,
		p2p.EVMCodeRequestHandlerID,
		peers,
	)
}
