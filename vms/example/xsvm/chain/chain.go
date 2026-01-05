// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/state"

	xsblock "github.com/ava-labs/avalanchego/vms/example/xsvm/block"
)

var _ Chain = (*chain)(nil)

type Chain interface {
	LastAccepted() ids.ID
	SetChainState(state snow.State)
	GetBlock(blkID ids.ID) (Block, error)

	// Creates a fully verifiable and executable block, which can be processed
	// by the consensus engine, from a stateless block.
	NewBlock(blk *xsblock.Stateless) (Block, error)
}

type chain struct {
	chainContext  *snow.Context
	acceptedState database.Database

	// chain state as driven by the consensus engine
	chainState snow.State

	lastAcceptedID ids.ID
	verifiedBlocks map[ids.ID]*block
}

func New(ctx *snow.Context, db database.Database) (Chain, error) {
	// Load the last accepted block data. For a newly created VM, this will be
	// the genesis. It is assumed the genesis was processed and stored
	// previously during VM initialization.
	lastAcceptedID, err := state.GetLastAccepted(db)
	if err != nil {
		return nil, err
	}

	c := &chain{
		chainContext:   ctx,
		acceptedState:  db,
		lastAcceptedID: lastAcceptedID,
	}

	lastAccepted, err := c.getBlock(lastAcceptedID)
	if err != nil {
		return nil, err
	}

	c.verifiedBlocks = map[ids.ID]*block{
		lastAcceptedID: lastAccepted,
	}
	return c, err
}

func (c *chain) LastAccepted() ids.ID {
	return c.lastAcceptedID
}

func (c *chain) SetChainState(state snow.State) {
	c.chainState = state
}

func (c *chain) GetBlock(blkID ids.ID) (Block, error) {
	return c.getBlock(blkID)
}

func (c *chain) NewBlock(blk *xsblock.Stateless) (Block, error) {
	blkID, err := blk.ID()
	if err != nil {
		return nil, err
	}

	if blk, exists := c.verifiedBlocks[blkID]; exists {
		return blk, nil
	}

	blkBytes, err := xsblock.Codec.Marshal(xsblock.CodecVersion, blk)
	if err != nil {
		return nil, err
	}

	return &block{
		Stateless: blk,
		chain:     c,
		id:        blkID,
		bytes:     blkBytes,
	}, nil
}

func (c *chain) getBlock(blkID ids.ID) (*block, error) {
	if blk, exists := c.verifiedBlocks[blkID]; exists {
		return blk, nil
	}

	blkBytes, err := state.GetBlock(c.acceptedState, blkID)
	if err != nil {
		return nil, err
	}

	stateless, err := xsblock.Parse(blkBytes)
	if err != nil {
		return nil, err
	}
	return &block{
		Stateless: stateless,
		chain:     c,
		id:        blkID,
		bytes:     blkBytes,
	}, nil
}
