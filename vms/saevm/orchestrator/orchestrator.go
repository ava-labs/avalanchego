package orchestrator

import (
	"context"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/libevm/core/types"
)

var _ adaptor.ChainVM[*blocks.Block] = (*Orchestrator[VMConfig, adaptor.SummaryProperties])(nil)

type Parser[T VMConfig] interface {
	ParseConfig([]byte) (T, error)
	ParseGenesis([]byte) (*types.Block, error)
}

type VMConfig interface {
	// TODO
	StateSyncConfig() StateSyncConfig
}

type ChainVM[T VMConfig] interface {
	health.Checker // TODO should we use this?
	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		db database.Database,
		genesisBytes []byte,
		config T,
		network *network.Network,
	) error

	SetState(ctx context.Context, state snow.State) error
	Shutdown(context.Context) error
	NewHTTPHandler(ctx context.Context) (http.Handler, error)
	WaitForEvent(ctx context.Context) (common.Message, error)

	ParseBlock(context.Context, []byte) (*blocks.Block, error)
	BuildBlock(context.Context, *block.Context) (*blocks.Block, error) // block.Context MAY be nil
	VerifyBlock(context.Context, *block.Context, *blocks.Block) error  // block.Context MAY be nil
	AcceptBlock(context.Context, *blocks.Block) error
	RejectBlock(context.Context, *blocks.Block) error
	SetPreference(context.Context, ids.ID, *block.Context) error // block.Context MAY be nil

	// These methods MUST be avaailable prior
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	LastAccepted(context.Context) (ids.ID, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)

	CreateHandlers(context.Context) (map[string]http.Handler, error)
	Version(context.Context) (string, error)
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
		genesis *types.Block,
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

type Orchestrator[C VMConfig, SP adaptor.SummaryProperties] struct {
	*network.Network
	vmLock sync.Mutex
	ChainVM[C]
	SummaryHandler[SP]

	parser Parser[C]
	// TODO Add hooks, state sync server fields.
	lazyChain func() error
}

// New constructs a new [Orchestrator] with the provided [ChainVM].
// The returned [Orchestrator] is not initialized. The provided [ChainVM]
// is not expected to be initialized, but it MUST be fully initialized after
// [ChainVM.Initialize].
func New[C VMConfig, SP adaptor.SummaryProperties](zeroVM ChainVM[C], parser Parser[C], summaryHandler SummaryHandler[SP]) *Orchestrator[C, SP] {
	return &Orchestrator[C, SP]{
		ChainVM:        zeroVM,
		SummaryHandler: summaryHandler,
		parser:         parser,
	}
}

func (o *Orchestrator[_, _]) Initialize(
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

	config, err := o.parser.ParseConfig(configBytes)
	if err != nil {
		return err
	}
	o.lazyChain = func() error {
		o.vmLock.Lock()
		defer o.vmLock.Unlock()

		if o.ChainVM != nil {
			return nil
		}

		return o.ChainVM.Initialize(ctx, snowCtx, db, genesisBytes, config, network)
	}

	g, err := o.parser.ParseGenesis(genesisBytes)
	if err != nil {
		return err
	}
	return o.SummaryHandler.Initialize(ctx, snowCtx, config.StateSyncConfig(), db, g)
}

func (o *Orchestrator[_, _]) Shutdown(ctx context.Context) error {
	if o.ChainVM == nil {
		return nil
	}
	return o.ChainVM.Shutdown(ctx)
}

func (o *Orchestrator[_, _]) SetState(ctx context.Context, state snow.State) error {
	if state == snow.Bootstrapping {
		if err := o.lazyChain(); err != nil {
			return err
		}
	}
	return o.ChainVM.SetState(ctx, state)
}

func (o *Orchestrator[_, _]) GetBlock(context.Context, ids.ID) (*blocks.Block, error) {
	panic("unimplemented")
}

func (o *Orchestrator[_, _]) LastAccepted(context.Context) (ids.ID, error) {
	panic("unimplemented")
}

func (o *Orchestrator[_, _]) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	panic("unimplemented")
}

func (o *Orchestrator[_, _]) WaitForEvent(context.Context) (common.Message, error) {
	panic("unimplemented")
}
