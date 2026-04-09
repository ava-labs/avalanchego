// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ chain = &VM{}

type chain interface {
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.SetPreferenceWithContextChainVM
}

type VM struct {
	transitionLock sync.RWMutex
	current        *current
}

type current struct {
	chain        chain
	requests     *requests
	connections  *connections
	httpHandlers *httpHandlers
}

func (v *VM) Initialize(ctx context.Context, chainCtx *snow.Context, db database.Database, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, fxs []*common.Fx, appSender common.AppSender) error {
	return errUnimplemented
}

func (v *VM) Shutdown(context.Context) error {
	return errUnimplemented
}

func (v *VM) SetState(ctx context.Context, state snow.State) error {
	return errUnimplemented
}

func (v *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	return 0, errUnimplemented
}

func (v *VM) BuildBlock(context.Context) (snowman.Block, error) {
	return nil, errUnimplemented
}

func (v *VM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	return nil, errUnimplemented
}

func (v *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	return nil, errUnimplemented
}

func (v *VM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	return nil, errUnimplemented
}

func (v *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return ids.ID{}, errUnimplemented
}
