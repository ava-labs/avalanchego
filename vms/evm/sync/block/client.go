// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// Client sends block-batch requests.
type Client = network.Dispatcher[*syncpb.GetBlockRequest, syncpb.GetBlockResponse, *syncpb.GetBlockResponse]

// NewClient binds a [Client] at [p2p.EVMBlockRequestHandlerID] on n.
func NewClient(log logging.Logger, n *p2p.Network, peers *p2p.PeerTracker) *Client {
	return network.NewDispatcher[*syncpb.GetBlockRequest, syncpb.GetBlockResponse, *syncpb.GetBlockResponse](
		log,
		n,
		p2p.EVMBlockRequestHandlerID,
		peers,
	)
}
