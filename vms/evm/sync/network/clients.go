// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/network/p2p"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// LeafClient sends leaf-range requests. Range-proof verification is the
// caller's responsibility.
type LeafClient = Dispatcher[*syncpb.GetLeafRequest, *syncpb.LeafResponse]

// CodeClient sends code-by-hash requests.
type CodeClient = Dispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse]

// BlockClient sends block-batch requests.
type BlockClient = Dispatcher[*syncpb.GetBlockRequest, *syncpb.BlockResponse]

func NewLeafClient(n *p2p.Network, peers *p2p.PeerTracker) *LeafClient {
	return NewDispatcher[*syncpb.GetLeafRequest, *syncpb.LeafResponse](
		n, p2p.EVMLeafsRequestHandlerID, peers,
	)
}

func NewCodeClient(n *p2p.Network, peers *p2p.PeerTracker) *CodeClient {
	return NewDispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse](
		n, p2p.EVMCodeRequestHandlerID, peers,
	)
}

func NewBlockClient(n *p2p.Network, peers *p2p.PeerTracker) *BlockClient {
	return NewDispatcher[*syncpb.GetBlockRequest, *syncpb.BlockResponse](
		n, p2p.EVMBlockRequestHandlerID, peers,
	)
}
