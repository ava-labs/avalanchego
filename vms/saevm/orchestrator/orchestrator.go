// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"context"
	"net/http"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
)

var _ adaptor.ChainVM[*blocks.Block] = (*orchestrator)(nil)

// ChainVM is the subset of [adaptor.ChainVM] that must be provided by the VM to
// work with the orchestrator.
type ChainVM interface {
	// Initialize is similar to [common.VM.Initialize], but abstracts network creation.
	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		db database.Database,
		genesisBytes []byte,
		configBytes []byte,
		network *network.Network,
	) error

	health.Checker
	SetState(ctx context.Context, state snow.State) error
	Shutdown(context.Context) error
	Version(context.Context) (string, error)
	CreateHandlers(context.Context) (map[string]http.Handler, error)
	NewHTTPHandler(ctx context.Context) (http.Handler, error)
	WaitForEvent(ctx context.Context) (common.Message, error)

	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	ParseBlock(context.Context, []byte) (*blocks.Block, error)
	BuildBlock(context.Context, *block.Context) (*blocks.Block, error) // block.Context MAY be nil
	VerifyBlock(context.Context, *block.Context, *blocks.Block) error  // block.Context MAY be nil
	AcceptBlock(context.Context, *blocks.Block) error
	RejectBlock(context.Context, *blocks.Block) error
	SetPreference(context.Context, ids.ID, *block.Context) error // block.Context MAY be nil
	LastAccepted(context.Context) (ids.ID, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
}

type orchestrator struct {
	*network.Network
	ChainVM

	initialized bool
}

// New constructs a new [adaptor.ChainVMWithContext] with the provided [ChainVM].
// The returned orchestrator is not initialized. The provided [ChainVM]
// is not expected to be initialized, but it MUST be fully initialized after
// [ChainVM.Initialize].
func New(zeroVM ChainVM) adaptor.ChainVMWithContext {
	return adaptor.Convert(&orchestrator{
		ChainVM: zeroVM,
	})
}

func (o *orchestrator) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte, // TODO need to parse for statesync
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	network, err := network.New(snowCtx, appSender)
	if err != nil {
		return err
	}
	o.Network = network
	if err := o.ChainVM.Initialize(ctx, snowCtx, db, genesisBytes, configBytes, network); err != nil {
		return err
	}
	o.initialized = true
	return nil
}

func (o *orchestrator) Shutdown(ctx context.Context) error {
	if !o.initialized {
		return nil
	}
	return o.ChainVM.Shutdown(ctx)
}
