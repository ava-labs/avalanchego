// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

// LeafFetcher reads leaf ranges over the message protocol. One [Client] serves both
// the state and atomic tries, so the node type is fixed per syncer, not per client.
type LeafFetcher struct {
	client   types.LeafClient
	reqType  message.LeafsRequestType
	nodeType message.NodeType
}

func NewLeafFetcher(c types.LeafClient, reqType message.LeafsRequestType, nodeType message.NodeType) *LeafFetcher {
	return &LeafFetcher{client: c, reqType: reqType, nodeType: nodeType}
}

func (f *LeafFetcher) FetchLeaves(ctx context.Context, req evmstate.LeafRange) (evmstate.Leaves, bool, error) {
	leafsReq, err := message.NewLeafsRequest(
		f.reqType, req.Root, req.Account, req.Start, req.End, req.Limit, f.nodeType,
	)
	if err != nil {
		return evmstate.Leaves{}, false, err
	}

	resp, err := f.client.GetLeafs(ctx, leafsReq)
	if err != nil {
		return evmstate.Leaves{}, false, err
	}
	return evmstate.Leaves{Keys: resp.Keys, Vals: resp.Vals}, resp.More, nil
}
