// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Bootstrapper implements the protocol used to determine the initial set of
// accepted blocks to sync to.
//
// The bootstrapping protocol starts by fetching the last accepted block from an
// initial subset of peers. In order for the protocol to find a recently
// accepted block, there must be at least one correct node in this subset of
// peers. If there is not a correct node in the subset of peers, the node will
// not accept an incorrect block. However, the node may be unable to find an
// acceptable block.
//
// Once the last accepted blocks have been fetched from the subset of peers, the
// set of blocks are sent to all peers. Each peer is expected to filter the
// provided blocks and report which of them they consider accepted. If a
// majority of the peers report that a block is accepted, then the node will
// consider that block to be accepted by the network. This assumes that a
// majority of the network is correct. If a majority of the network is
// malicious, the node may accept an incorrect block.
type Bootstrapper interface {
	// GetAcceptedFrontiersToSend returns the set of peers whose accepted
	// frontier should be requested. It is expected to repeatedly call this
	// function along with [RecordAcceptedFrontier] until [GetAcceptedFrontier]
	// returns that the frontier is finalized.
	GetAcceptedFrontiersToSend(ctx context.Context) (peers set.Set[ids.NodeID])
	// RecordAcceptedFrontier of nodes whose accepted frontiers were requested.
	// [blkIDs] is typically either empty, if the request for frontiers failed,
	// or a single block.
	RecordAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, blkIDs ...ids.ID)
	// GetAcceptedFrontier returns the union of all the provided frontiers along
	// with a flag to identify that the frontier has finished being calculated.
	GetAcceptedFrontier(ctx context.Context) (blkIDs []ids.ID, finalized bool)

	// GetAcceptedFrontiersToSend returns the set of peers who should be
	// requested to filter the frontier. It is expected to repeatedly call this
	// function along with [RecordAccepted] until [GetAccepted] returns that the
	// set is finalized.
	GetAcceptedToSend(ctx context.Context) (peers set.Set[ids.NodeID])
	// RecordAccepted blocks of nodes that were requested. [blkIDs] should
	// typically be a subset of the frontier. Any returned error should be
	// treated as fatal.
	RecordAccepted(ctx context.Context, nodeID ids.NodeID, blkIDs []ids.ID) error
	// GetAccepted returns a set of accepted blocks along with a flag to
	// identify that the set has finished being calculated.
	GetAccepted(ctx context.Context) (blkIDs []ids.ID, finalized bool)
}
