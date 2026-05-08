// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/network/p2p"
	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// LeafsClient sends leaf-range requests. Range-proof verification is the
// caller's responsibility.
type LeafsClient = Dispatcher[*syncpb.GetLeafsRequest, *syncpb.LeafsResponse]

// CodeClient sends code-by-hash requests.
type CodeClient = Dispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse]

// BlockClient sends block-batch requests.
type BlockClient = Dispatcher[*syncpb.GetBlockRequest, *syncpb.BlockResponse]

func NewLeafsClient(p2pNet *p2p.Network, peers *p2p.PeerTracker) *LeafsClient {
	return NewDispatcher[*syncpb.GetLeafsRequest, *syncpb.LeafsResponse](
		p2pNet, p2p.EVMLeafsRequestHandlerID, peers,
	)
}

func NewCodeClient(p2pNet *p2p.Network, peers *p2p.PeerTracker) *CodeClient {
	return NewDispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse](
		p2pNet, p2p.EVMCodeRequestHandlerID, peers,
	)
}

func NewBlockClient(p2pNet *p2p.Network, peers *p2p.PeerTracker) *BlockClient {
	return NewDispatcher[*syncpb.GetBlockRequest, *syncpb.BlockResponse](
		p2pNet, p2p.EVMBlockRequestHandlerID, peers,
	)
}
