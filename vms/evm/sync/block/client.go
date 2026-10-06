// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// Client sends block-batch requests.
type Client = network.Dispatcher[*syncpb.GetBlockRequest, syncpb.GetBlockResponse, *syncpb.GetBlockResponse, []*types.Block]

// NewClient returns a [Client] sending through client, which must be bound to
// [p2p.EVMBlockRequestHandlerID].
func NewClient(log logging.Logger, client *p2p.TrackingClient) *Client {
	return network.NewDispatcher[*syncpb.GetBlockRequest, syncpb.GetBlockResponse, *syncpb.GetBlockResponse, []*types.Block](log, client)
}
