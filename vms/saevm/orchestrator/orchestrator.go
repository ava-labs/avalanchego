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

type StateSyncConfig struct {
	Enabled     *bool
	NodeIDs     []ids.NodeID
	StateScheme string
}

// SummaryHandler TODO integrate with Orchestrator.
type SummaryHandler[SP adaptor.SummaryProperties] interface {
	// Initialize provides the summary handler will all details necessary to
	// state sync at [common.VM.Initialize] time.
	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		cfg StateSyncConfig,
		db database.Database,
	) error

	// GetBlock is the same as [ChainVM.GetBlock], but the block will only be
	// used for its [adaptor.BlockProperties].
	// Returns [database.ErrNotFound] if the block is not found.
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	// LastAccepted returns the ID of the last accepted block on any previous
	// run.
	// TODO(alarso16): How do I return the genesis ID as defined in [common.VM.LastAccepted]?
	LastAccepted(context.Context) (ids.ID, error)
	// GetBlockIDAtHeight [database.ErrNotFound] if a block at [height] is not found.
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
	// WaitForEvent will only be called if [SummaryHandler.AcceptSummary] returns
	// [block.StateSyncStatic]. This function should block until the state sync is complete.
	WaitForEvent(context.Context) (common.Message, error)
	// Shutdown cancels any pending state sync operations and returns once they are complete.
	Shutdown(context.Context) error

	// StateSyncEnabled returns true if the summary handler expects
	StateSyncEnabled(context.Context) (bool, error)
	GetLastStateSummary(context.Context) (SP, error)
	GetOngoingSyncStateSummary(context.Context) (SP, error)
	GetStateSummary(context.Context, uint64) (SP, error)
	ParseStateSummary(context.Context, []byte) (SP, error)
	AcceptSummary(context.Context, SP) (block.StateSyncMode, error)
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
