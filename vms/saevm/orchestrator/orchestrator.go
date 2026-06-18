package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/ava-labs/libevm/core/types"

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

var _ adaptor.ChainVM[*blocks.Block] = (*Orchestrator[adaptor.SummaryProperties])(nil)

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

// ChainVM is the executing chain the orchestrator builds and routes to.
type ChainVM interface {
	health.Checker // TODO should we use this?
	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		db database.Database,
		genesisBytes []byte,
		config VMConfig,
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

	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	LastAccepted(context.Context) (ids.ID, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)

	CreateHandlers(context.Context) (map[string]http.Handler, error)
	Version(context.Context) (string, error)
}

// StateSyncConfig configures state sync.
type StateSyncConfig struct {
	Enabled     *bool
	NodeIDs     []ids.NodeID
	StateScheme string
}

// SummaryHandler is the state syncable component. It serves summaries and the
// disk-backed block methods during sync and signals completion via WaitForEvent.
type SummaryHandler[SP adaptor.SummaryProperties] interface {
	// Initialize provides the details needed to state sync.
	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		cfg StateSyncConfig,
		db database.Database,
		genesis *types.Block,
	) error

	// GetBlock returns the block with the given ID, or [database.ErrNotFound].
	// Only the block's [adaptor.BlockProperties] are used.
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	// LastAccepted returns the ID of the last accepted block from a previous run.
	// TODO(alarso16): How do I return the genesis ID as defined in [common.VM.LastAccepted]?
	LastAccepted(context.Context) (ids.ID, error)
	// GetBlockIDAtHeight returns the block ID at height, or [database.ErrNotFound].
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
	// WaitForEvent blocks until state sync completes. It is called only after
	// AcceptSummary returns [block.StateSyncStatic].
	WaitForEvent(context.Context) (common.Message, error)
	// Shutdown cancels any in-flight state sync and waits for it to stop.
	Shutdown(context.Context) error

	// StateSyncEnabled reports whether state sync is enabled.
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
	config       VMConfig
}

// Orchestrator is the single VM the engine sees. It routes each call to the state
// syncable component while syncing and to the executing chain once built.
type Orchestrator[SP adaptor.SummaryProperties] struct {
	*network.Network
	ChainVM
	SummaryHandler[SP]

	parser Parser

	// vmLock guards state, built, and the build. built is set only on a
	// successful build, so a failed build is retried on the next transition.
	vmLock   sync.Mutex
	state    snow.State
	built    bool
	deferred deferredBuild
}

// New returns an uninitialized [Orchestrator]. Initialize must be called before use.
func New[SP adaptor.SummaryProperties](zeroVM ChainVM, parser Parser, summaryHandler SummaryHandler[SP]) *Orchestrator[SP] {
	return &Orchestrator[SP]{
		ChainVM:        zeroVM,
		SummaryHandler: summaryHandler,
		parser:         parser,
	}
}

// Initialize creates the shared network, initializes the summary handler, and
// captures the inputs for the chain build deferred to bootstrapping.
func (o *Orchestrator[_]) Initialize(
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
	// The build uses the live SetState ctx, not this one, so a shutdown during
	// the build aborts cleanly.
	o.deferred = deferredBuild{
		snowCtx:      snowCtx,
		db:           db,
		genesisBytes: genesisBytes,
		config:       config,
	}

	g, err := o.parser.ParseGenesis(genesisBytes)
	if err != nil {
		return err
	}
	return o.SummaryHandler.Initialize(ctx, snowCtx, config.StateSyncConfig(), db, g)
}

// Shutdown releases the summary handler always and the chain if it was built.
func (o *Orchestrator[_]) Shutdown(ctx context.Context) error {
	errs := []error{o.SummaryHandler.Shutdown(ctx)}
	if c, _, ok := o.snapshot(); ok {
		errs = append(errs, c.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

// SetState builds the chain on the first transition into bootstrapping, installs
// it before advancing, then forwards the state to it. The build never runs during
// state syncing, and a failed build is retried on the next transition.
func (o *Orchestrator[_]) SetState(ctx context.Context, state snow.State) error {
	if state >= snow.Bootstrapping {
		if err := o.ensureChain(ctx); err != nil {
			return err
		}
	}
	if o.advance(state) {
		return o.ChainVM.SetState(ctx, state)
	}
	return nil
}

// ensureChain builds the chain at most once, under the lock so no reader sees a
// half-installed chain. A failed build is retryable.
func (o *Orchestrator[_]) ensureChain(ctx context.Context) error {
	o.vmLock.Lock()
	defer o.vmLock.Unlock()
	if o.built {
		return nil
	}
	b := o.deferred
	if err := o.ChainVM.Initialize(ctx, b.snowCtx, b.db, b.genesisBytes, b.config, o.Network); err != nil {
		return err
	}
	o.built = true
	return nil
}

// advance records the new lifecycle state and reports whether the chain is built.
func (o *Orchestrator[_]) advance(state snow.State) bool {
	o.vmLock.Lock()
	defer o.vmLock.Unlock()
	o.state = state
	return o.built
}

// snapshot reads the routing target, lifecycle state, and built flag together.
// The chain is nil unless built.
func (o *Orchestrator[_]) snapshot() (ChainVM, snow.State, bool) {
	o.vmLock.Lock()
	defer o.vmLock.Unlock()
	if !o.built {
		return nil, o.state, false
	}
	return o.ChainVM, o.state, true
}

// errChainNotBuilt is returned by the block-production methods before the chain
// is built.
var errChainNotBuilt = errors.New("chain not built")

// GetBlock returns the block with the given ID, from the handler before the
// chain is built and from the chain after.
func (o *Orchestrator[_]) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	if c, _, ok := o.snapshot(); ok {
		return c.GetBlock(ctx, id)
	}
	return o.SummaryHandler.GetBlock(ctx, id)
}

// LastAccepted returns the last accepted block ID, from the handler before the
// chain is built and from the chain after.
func (o *Orchestrator[_]) LastAccepted(ctx context.Context) (ids.ID, error) {
	if c, _, ok := o.snapshot(); ok {
		return c.LastAccepted(ctx)
	}
	return o.SummaryHandler.LastAccepted(ctx)
}

// GetBlockIDAtHeight returns the block ID at the given height, from the handler
// before the chain is built and from the chain after.
func (o *Orchestrator[_]) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if c, _, ok := o.snapshot(); ok {
		return c.GetBlockIDAtHeight(ctx, height)
	}
	return o.SummaryHandler.GetBlockIDAtHeight(ctx, height)
}

// ParseBlock parses the block bytes, forwarding to the chain once built and
// returning errChainNotBuilt before.
func (o *Orchestrator[_]) ParseBlock(ctx context.Context, b []byte) (*blocks.Block, error) {
	if c, _, ok := o.snapshot(); ok {
		return c.ParseBlock(ctx, b)
	}
	return nil, errChainNotBuilt
}

// BuildBlock builds a block, forwarding to the chain once built and returning
// errChainNotBuilt before.
func (o *Orchestrator[_]) BuildBlock(ctx context.Context, bCtx *block.Context) (*blocks.Block, error) {
	if c, _, ok := o.snapshot(); ok {
		return c.BuildBlock(ctx, bCtx)
	}
	return nil, errChainNotBuilt
}

// VerifyBlock verifies a block, forwarding to the chain once built and returning
// errChainNotBuilt before.
func (o *Orchestrator[_]) VerifyBlock(ctx context.Context, bCtx *block.Context, b *blocks.Block) error {
	if c, _, ok := o.snapshot(); ok {
		return c.VerifyBlock(ctx, bCtx, b)
	}
	return errChainNotBuilt
}

// AcceptBlock accepts a block, forwarding to the chain once built and returning
// errChainNotBuilt before.
func (o *Orchestrator[_]) AcceptBlock(ctx context.Context, b *blocks.Block) error {
	if c, _, ok := o.snapshot(); ok {
		return c.AcceptBlock(ctx, b)
	}
	return errChainNotBuilt
}

// RejectBlock rejects a block, forwarding to the chain once built and returning
// errChainNotBuilt before.
func (o *Orchestrator[_]) RejectBlock(ctx context.Context, b *blocks.Block) error {
	if c, _, ok := o.snapshot(); ok {
		return c.RejectBlock(ctx, b)
	}
	return errChainNotBuilt
}

// SetPreference sets the preferred block, forwarding to the chain once built and
// returning errChainNotBuilt before.
func (o *Orchestrator[_]) SetPreference(ctx context.Context, id ids.ID, bCtx *block.Context) error {
	if c, _, ok := o.snapshot(); ok {
		return c.SetPreference(ctx, id, bCtx)
	}
	return errChainNotBuilt
}

// WaitForEvent multiplexes the single engine subscription: the built chain, the
// summary handler while syncing, or a wait until the next transition otherwise.
// It must not signal completion while initializing, since the no-sync path never
// enters state syncing.
func (o *Orchestrator[_]) WaitForEvent(ctx context.Context) (common.Message, error) {
	c, state, ok := o.snapshot()
	switch {
	case ok:
		return c.WaitForEvent(ctx)
	case state == snow.StateSyncing:
		return o.SummaryHandler.WaitForEvent(ctx)
	default:
		<-ctx.Done()
		return 0, ctx.Err()
	}
}
