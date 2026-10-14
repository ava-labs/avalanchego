// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
)

// LeafFetcher reads leaf ranges over the message protocol. One [Client] serves both
// the state and atomic tries, so the node type is fixed per syncer, not per client.
type LeafFetcher struct {
	client   types.LeafClient
	nodeType message.NodeType
}

func NewLeafFetcher(c types.LeafClient, nodeType message.NodeType) *LeafFetcher {
	return &LeafFetcher{
		client:   c,
		nodeType: nodeType,
	}
}

func (f *LeafFetcher) FetchLeaves(ctx context.Context, req hashdb.LeafRange) (hashdb.Leaves, bool, error) {
	var account common.Hash
	if req.Account != nil {
		account = *req.Account
	}

	// End is always nil, [hashdb.LeafRange] carries no end key.
	resp, err := f.client.GetLeafs(ctx, message.SubnetEVMLeafsRequest{
		Root:     req.Root,
		Account:  account,
		Start:    req.Start,
		Limit:    req.Limit,
		NodeType: f.nodeType,
	})
	if err != nil {
		return hashdb.Leaves{}, false, err
	}
	return hashdb.Leaves{
		Keys: resp.Keys,
		Vals: resp.Vals,
	}, resp.More, nil
}
