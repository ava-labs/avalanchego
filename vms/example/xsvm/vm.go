// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/version"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/xsvm/api"
	"github.com/ava-labs/xsvm/builder"
	"github.com/ava-labs/xsvm/chain"
	"github.com/ava-labs/xsvm/execute"
	"github.com/ava-labs/xsvm/genesis"

	xsblock "github.com/ava-labs/xsvm/block"
)

var (
	_ smblock.ChainVM                      = (*VM)(nil)
	_ smblock.BuildBlockWithContextChainVM = (*VM)(nil)
)

type VM struct {
	chainContext *snow.Context
	db           database.Database
	genesis      *genesis.Genesis
	engineChan   chan<- common.Message

	chain   chain.Chain
	builder builder.Builder
}

func (vm *VM) Initialize(
	_ context.Context,
	chainContext *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	_ []byte,
	_ []byte,
	engineChan chan<- common.Message,
	_ []*common.Fx,
	_ common.AppSender,
) error {
	chainContext.Log.Info("initializing xsvm",
		zap.Stringer("version", Version),
	)

	vm.chainContext = chainContext
	vm.db = dbManager.Current().Database

	g, err := genesis.Parse(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis bytes: %w", err)
	}

	vdb := versiondb.New(vm.db)
	if err := execute.Genesis(vdb, chainContext.ChainID, g); err != nil {
		return fmt.Errorf("failed to initialize genesis state: %w", err)
	}
	if err := vdb.Commit(); err != nil {
		return err
	}

	vm.genesis = g
	vm.engineChan = engineChan

	vm.chain, err = chain.New(chainContext, vm.db)
	if err != nil {
		return fmt.Errorf("failed to initialize chain manager: %w", err)
	}

	vm.builder = builder.New(chainContext, engineChan, vm.chain)

	chainContext.Log.Info("initialized xsvm",
		zap.Stringer("lastAcceptedID", vm.chain.LastAccepted()),
	)
	return nil
}

func (vm *VM) SetState(_ context.Context, state snow.State) error {
	vm.chain.SetChainState(state)
	return nil
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.chainContext == nil {
		return nil
	}
	return vm.db.Close()
}

func (*VM) Version(context.Context) (string, error) {
	return Version.String(), nil
}

func (*VM) CreateStaticHandlers(context.Context) (map[string]*common.HTTPHandler, error) {
	return nil, nil
}

func (vm *VM) CreateHandlers(_ context.Context) (map[string]*common.HTTPHandler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	api := api.NewServer(
		vm.chainContext,
		vm.genesis,
		vm.db,
		vm.chain,
		vm.builder,
	)
	if err := server.RegisterService(api, Name); err != nil {
		return nil, err
	}
	return map[string]*common.HTTPHandler{
		"": {
			LockOptions: common.WriteLock,
			Handler:     server,
		},
	}, nil
}

func (*VM) AppRequest(context.Context, ids.NodeID, uint32, time.Time, []byte) error {
	return nil
}

func (*VM) AppRequestFailed(context.Context, ids.NodeID, uint32) error {
	return nil
}

func (*VM) AppResponse(context.Context, ids.NodeID, uint32, []byte) error {
	return nil
}

func (*VM) AppGossip(context.Context, ids.NodeID, []byte) error {
	return nil
}

func (*VM) CrossChainAppRequest(context.Context, ids.ID, uint32, time.Time, []byte) error {
	return nil
}

func (*VM) CrossChainAppRequestFailed(context.Context, ids.ID, uint32) error {
	return nil
}

func (*VM) CrossChainAppResponse(context.Context, ids.ID, uint32, []byte) error {
	return nil
}

func (*VM) HealthCheck(context.Context) (interface{}, error) {
	return http.StatusOK, nil
}

func (*VM) Connected(context.Context, ids.NodeID, *version.Application) error {
	return nil
}

func (*VM) Disconnected(context.Context, ids.NodeID) error {
	return nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	return vm.chain.GetBlock(blkID)
}

func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (snowman.Block, error) {
	blk, err := xsblock.Parse(blkBytes)
	if err != nil {
		return nil, err
	}
	return vm.chain.NewBlock(blk)
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return vm.builder.BuildBlock(ctx, nil)
}

func (vm *VM) SetPreference(_ context.Context, preferred ids.ID) error {
	vm.builder.SetPreference(preferred)
	return nil
}

func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.chain.LastAccepted(), nil
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, blockContext *smblock.Context) (snowman.Block, error) {
	return vm.builder.BuildBlock(ctx, blockContext)
}
