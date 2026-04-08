// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"net/http"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
)

var _ chain = &VM{}

type chain interface {
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.SetPreferenceWithContextChainVM
}

type VM struct {
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

func (v *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return nil, errUnimplemented
}

func (v *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, errUnimplemented
}

func (v *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return errUnimplemented
}

func (v *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return errUnimplemented
}

func (v *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	return errUnimplemented
}

func (v *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return errUnimplemented
}

func (v *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return errUnimplemented
}

func (v *VM) SetPreferenceWithContext(ctx context.Context, blkID ids.ID, blockCtx *block.Context) error {
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
