// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"context"
	"errors"
	"net/http"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
)

var _ adaptor.ChainVM[*blocks.Block] = (*orchestrator[adaptor.SummaryProperties])(nil)

// Parser turns the raw config and genesis bytes into the typed values the
// orchestrator and its collaborators need.
type Parser interface {
	ParseConfig([]byte) (VMConfig, error)
	ParseGenesis([]byte) (*types.Block, error)
}

// VMConfig is the parsed VM configuration.
type VMConfig interface {
	StateSyncConfig() StateSyncConfig // TODO
}

// StateDependent contains all methods that route to the summary handler until
// the chain is built, then route to the chain. This allows the engine to
// startup without fully initializing the VM.
type StateDependent interface {
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	LastAccepted(context.Context) (ids.ID, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
	WaitForEvent(context.Context) (common.Message, error)
	Shutdown(context.Context) error
}

// ChainVM is the executing chain the orchestrator builds and routes to.
type ChainVM interface {
	StateDependent

	// Initialize is similar to [common.VM.Initialize], but abstracts network creation.
	// All other methods MUST be callable after Initialize.
	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		db database.Database,
		genesisBytes []byte,
		configBytes []byte,
		network *network.Network,
	) error

	// TODO(alarso16): this needs to be callable immediately after Initialize.
	CreateHandlers(context.Context) (map[string]http.Handler, error)

	// All other methods are exactly like [adaptor.ChainVM].
	health.Checker
	SetState(ctx context.Context, state snow.State) error
	NewHTTPHandler(ctx context.Context) (http.Handler, error)

	ParseBlock(context.Context, []byte) (*blocks.Block, error)
	BuildBlock(context.Context, *block.Context) (*blocks.Block, error) // block.Context MAY be nil
	SetPreference(context.Context, ids.ID, *block.Context) error       // block.Context MAY be nil
	VerifyBlock(context.Context, *block.Context, *blocks.Block) error  // block.Context MAY be nil
	RejectBlock(context.Context, *blocks.Block) error
	AcceptBlock(context.Context, *blocks.Block) error

	Version(context.Context) (string, error)
}

// StateSyncConfig configures state sync.
type StateSyncConfig struct {
	Enabled        *bool
	NodeIDs        []ids.NodeID
	StateScheme    string
	CommitInterval uint64
}

// SummaryHandler is the state syncable component. It serves summaries and the
// disk-backed block methods during sync and signals completion via WaitForEvent.
type SummaryHandler[SP adaptor.SummaryProperties] interface {
	StateDependent

	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		cfg StateSyncConfig,
		db database.Database,
		genesis *types.Block,
	) error
	StateSyncEnabled(context.Context) (bool, error)
	GetLastStateSummary(context.Context) (SP, error)
	GetOngoingSyncStateSummary(context.Context) (SP, error)
	GetStateSummary(context.Context, uint64) (SP, error)
	ParseStateSummary(context.Context, []byte) (SP, error)
	AcceptSummary(context.Context, SP) (block.StateSyncMode, error)
}

// deferredBuild holds the inputs captured at Initialize for the late chain build.
type deferredBuild struct {
	snowCtx      *snow.Context
	db           database.Database
	genesisBytes []byte
	configBytes  []byte
}

// orchestrator is the single VM the engine sees. It routes each call to the state
// syncable component while syncing and to the executing chain once built.
type orchestrator[SP adaptor.SummaryProperties] struct {
	*network.Network
	ChainVM
	SummaryHandler[SP]

	parser Parser

	state           utils.Atomic[snow.State] // zero value is [snow.Initializing]
	deferred        deferredBuild
	syncInitialized bool
}

// New constructs a new [adaptor.ChainVMWithContext] with the provided [ChainVM].
// The returned orchestrator is not initialized. The provided [ChainVM]
// is not expected to be initialized, but it MUST be fully initialized after
// [ChainVM.Initialize].
func New(zeroVM ChainVM) adaptor.ChainVMWithContext {
	return adaptor.Convert(&orchestrator[adaptor.SummaryProperties]{
		ChainVM: zeroVM,
	})
}

type shim struct {
	adaptor.ChainVMWithContext
	block.StateSyncableVM
}

// NewStateSyncable constructs a new [adaptor.ChainVMWithContext] with the provided [ChainVM].
// The returned orchestrator is not initialized. The provided [ChainVM]
// is not expected to be initialized, but it MUST be fully initialized after
// [ChainVM.Initialize].
// The returned struct additionally implements [block.StateSyncableVM].
func NewStateSyncable[SP adaptor.SummaryProperties](zeroVM ChainVM, summaryHandler SummaryHandler[SP], parser Parser) adaptor.ChainVMWithContext {
	o := &orchestrator[SP]{
		SummaryHandler: summaryHandler,
		ChainVM:        zeroVM,
		parser:         parser,
	}
	return shim{
		ChainVMWithContext: adaptor.Convert(o),
		StateSyncableVM:    adaptor.ConvertStateSync(o),
	}
}

// Initialize creates the shared network, initializes the summary handler, and
// captures the inputs for the chain build deferred to bootstrapping.
func (o *orchestrator[_]) Initialize(
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

	// Capture for eventual chain initialize
	o.deferred = deferredBuild{
		snowCtx:      snowCtx,
		db:           db,
		genesisBytes: genesisBytes,
		configBytes:  configBytes,
	}

	// Allow no summary handler for testing.
	if o.SummaryHandler == nil {
		return nil
	}

	config, err := o.parser.ParseConfig(configBytes)
	if err != nil {
		return err
	}

	g, err := o.parser.ParseGenesis(genesisBytes)
	if err != nil {
		return err
	}
	if err := o.SummaryHandler.Initialize(ctx, snowCtx, config.StateSyncConfig(), db, g); err != nil {
		return err
	}
	o.syncInitialized = true

	return nil
}

func (o *orchestrator[_]) StateSyncEnabled(ctx context.Context) (bool, error) {
	if o.SummaryHandler == nil {
		return false, nil
	}
	return o.SummaryHandler.StateSyncEnabled(ctx)
}

// Shutdown releases the summary handler always and the chain if it was built.
func (o *orchestrator[_]) Shutdown(ctx context.Context) error {
	var err error
	if o.state.Get() >= snow.Bootstrapping {
		err = o.ChainVM.Shutdown(ctx)
	}
	if o.syncInitialized {
		err = errors.Join(err, o.SummaryHandler.Shutdown(ctx))
	}
	return err
}

func (o *orchestrator[_]) SetState(ctx context.Context, state snow.State) error {
	prev := o.state.Get()
	defer o.state.Set(state) // In defer to guarantee ChainVM is initialized first

	if state < snow.Bootstrapping {
		return nil
	}

	if prev < snow.Bootstrapping {
		b := o.deferred
		if err := o.ChainVM.Initialize(ctx, b.snowCtx, b.db, b.genesisBytes, b.configBytes, o.Network); err != nil {
			return err
		}
	}
	return o.ChainVM.SetState(ctx, state)
}

func (o *orchestrator[_]) activeHandler() StateDependent {
	if o.state.Get() >= snow.Bootstrapping || o.SummaryHandler == nil {
		return o.ChainVM
	}
	return o.SummaryHandler
}

func (o *orchestrator[_]) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	return o.activeHandler().GetBlock(ctx, id)
}

func (o *orchestrator[_]) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return o.activeHandler().GetBlockIDAtHeight(ctx, height)
}

func (o *orchestrator[_]) LastAccepted(ctx context.Context) (ids.ID, error) {
	return o.activeHandler().LastAccepted(ctx)
}

func (o *orchestrator[_]) WaitForEvent(ctx context.Context) (common.Message, error) {
	return o.activeHandler().WaitForEvent(ctx)
}
